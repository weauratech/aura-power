package notifications

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

const (
	// AuditOutboxLabel marks audit records created under the durable handoff
	// contract. It prevents an upgrade from replaying historical audit events.
	AuditOutboxLabel          = "power.aura.sh/notification-outbox"
	AuditOutboxEnabled        = "enabled"
	deliveryPolicyAtMostOnce  = "at-most-once"
	deliveryPolicyAtLeastOnce = "at-least-once"
	deliveryClaimTimeout      = 30 * time.Second
	defaultMaxAttempts        = int32(5)
)

// SetupWithManager reconciles durable delivery records independently from
// workload reconciliation. Controller-runtime leader election ensures only the
// active controller instance performs provider side effects.
func (d *Dispatcher) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		Named("notification-delivery").
		For(&v1alpha1.PowerNotificationDelivery{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Complete(d); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("notification-audit-materializer").
		For(&v1alpha1.PowerAuditEvent{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(&auditMaterializer{dispatcher: d})
}

type auditMaterializer struct{ dispatcher *Dispatcher }

func (r *auditMaterializer) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.dispatcher.controlNamespace != "" && req.Namespace != r.dispatcher.controlNamespace {
		return ctrl.Result{}, nil
	}
	var audit v1alpha1.PowerAuditEvent
	if err := r.dispatcher.secretReader.Get(ctx, req.NamespacedName, &audit); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if audit.Labels[AuditOutboxLabel] != AuditOutboxEnabled || audit.Spec.NotificationSuppressed {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.dispatcher.enqueueDurable(ctx, Event{AuditEventRef: audit.Namespace + "/" + audit.Name})
}

// enqueueDurable materializes one immutable outbox record per audit/channel
// pair. All provider filtering is decided here; delivery never needs to hold up
// the workload reconciler after these Kubernetes writes succeed.
func (d *Dispatcher) enqueueDurable(ctx context.Context, event Event) error {
	parts := strings.Split(event.AuditEventRef, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid audit event reference %q", event.AuditEventRef)
	}
	var audit v1alpha1.PowerAuditEvent
	if err := d.secretReader.Get(ctx, client.ObjectKey{Namespace: parts[0], Name: parts[1]}, &audit); err != nil {
		return fmt.Errorf("read audit event for durable notification: %w", err)
	}
	if audit.UID == "" {
		// The Kubernetes API always assigns UIDs. This deterministic fallback is
		// retained for fake clients used by embedders and tests.
		audit.UID = types.UID("name:" + audit.Namespace + "/" + audit.Name)
	}
	// The persisted audit is the source of truth. Do not let an in-process
	// caller select channels with data that differs from the durable record.
	event = Event{
		AuditEventRef: audit.Namespace + "/" + audit.Name,
		Action:        audit.Spec.Action, Result: audit.Spec.Result, Reason: audit.Spec.Reason,
		RuleName: audit.Spec.RuleName, Timestamp: audit.Spec.Timestamp.Time,
		Target: TargetRef{Namespace: audit.Spec.Target.Namespace, Name: audit.Spec.Target.Name, Kind: audit.Spec.Target.Kind, UID: audit.Spec.Target.UID},
	}

	var channels v1alpha1.PowerNotificationChannelList
	if err := d.listChannels(ctx, &channels); err != nil {
		return fmt.Errorf("list channels for durable notification: %w", err)
	}
	var errs []error
	for i := range channels.Items {
		channel := &channels.Items[i]
		if !channel.Spec.Enabled || !channelMatchesEvent(channel, event) {
			continue
		}
		if channel.UID == "" {
			channel.UID = types.UID("name:" + channel.Namespace + "/" + channel.Name)
		}
		delivery := deliveryFor(event, &audit, channel)
		if err := d.client.Create(ctx, delivery); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				errs = append(errs, fmt.Errorf("create delivery %s: %w", delivery.Name, err))
				continue
			}
			var existing v1alpha1.PowerNotificationDelivery
			if getErr := d.client.Get(ctx, client.ObjectKeyFromObject(delivery), &existing); getErr != nil {
				errs = append(errs, fmt.Errorf("verify delivery %s: %w", delivery.Name, getErr))
				continue
			}
			if !deliverySpecsEqual(existing.Spec, delivery.Spec) {
				errs = append(errs, fmt.Errorf("delivery %s already exists with different semantics", delivery.Name))
			}
		}
	}
	err := errors.Join(errs...)
	select {
	case d.outboxWake <- struct{}{}:
	default:
	}
	return err
}

func deliverySpecsEqual(left, right v1alpha1.PowerNotificationDeliverySpec) bool {
	if !left.Event.Timestamp.Time.Equal(right.Event.Timestamp.Time) {
		return false
	}
	left.Event.Timestamp = metav1.Time{}
	right.Event.Timestamp = metav1.Time{}
	return equality.Semantic.DeepEqual(left, right)
}

func channelMatchesEvent(channel *v1alpha1.PowerNotificationChannel, event Event) bool {
	if len(channel.Spec.Events) > 0 && !contains(channel.Spec.Events, event.Action) {
		return false
	}
	return len(channel.Spec.NamespaceFilter) == 0 || contains(channel.Spec.NamespaceFilter, event.Target.Namespace)
}

func deliveryFor(event Event, audit *v1alpha1.PowerAuditEvent, channel *v1alpha1.PowerNotificationChannel) *v1alpha1.PowerNotificationDelivery {
	seed := string(audit.UID) + "\x00" + string(channel.UID)
	digest := sha256.Sum256([]byte(seed))
	name := fmt.Sprintf("delivery-%x", digest[:16])
	throttleDigest := sha256.Sum256([]byte(eventIdentity(event)))
	auditLabelDigest := sha256.Sum256([]byte(string(audit.UID)))
	channelLabelDigest := sha256.Sum256([]byte(string(channel.UID)))
	policy := channel.Spec.DeliveryPolicy
	if policy == "" {
		policy = deliveryPolicyAtMostOnce
	}
	maxAttempts := channel.Spec.MaxDeliveryAttempts
	if maxAttempts == 0 {
		maxAttempts = defaultMaxAttempts
	}
	if policy == deliveryPolicyAtMostOnce {
		maxAttempts = 1
	}
	controller := true
	block := true
	return &v1alpha1.PowerNotificationDelivery{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: audit.Namespace,
			Labels: map[string]string{
				"power.aura.sh/audit-event":  fmt.Sprintf("uid-%x", auditLabelDigest[:16]),
				"power.aura.sh/channel":      fmt.Sprintf("uid-%x", channelLabelDigest[:16]),
				"power.aura.sh/throttle-key": fmt.Sprintf("key-%x", throttleDigest[:16]),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: v1alpha1.GroupVersion.String(), Kind: "PowerAuditEvent",
				Name: audit.Name, UID: audit.UID, Controller: &controller, BlockOwnerDeletion: &block,
			}},
		},
		Spec: v1alpha1.PowerNotificationDeliverySpec{
			AuditEvent: v1alpha1.NotificationObjectReference{Name: audit.Name, UID: string(audit.UID)},
			Channel:    v1alpha1.NotificationObjectReference{Name: channel.Name, UID: string(channel.UID)},
			Event: v1alpha1.NotificationEventSnapshot{
				Action: audit.Spec.Action, Result: audit.Spec.Result, Reason: audit.Spec.Reason, RuleName: audit.Spec.RuleName,
				Timestamp: metav1.NewTime(audit.Spec.Timestamp.Time.UTC().Truncate(time.Second)),
				Target:    audit.Spec.Target,
			},
			IdempotencyKey: "aura-power/" + name,
			DeliveryPolicy: policy, MaxAttempts: maxAttempts,
		},
	}
}

func (d *Dispatcher) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if d.controlNamespace != "" && req.Namespace != d.controlNamespace {
		return ctrl.Result{}, nil
	}
	var delivery v1alpha1.PowerNotificationDelivery
	if err := d.client.Get(ctx, req.NamespacedName, &delivery); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	now := time.Now().UTC()
	switch delivery.Status.Phase {
	case v1alpha1.NotificationDeliverySucceeded, v1alpha1.NotificationDeliveryFailed, v1alpha1.NotificationDeliveryAmbiguous:
		return ctrl.Result{}, d.ensureChannelStatus(ctx, &delivery)
	case v1alpha1.NotificationDeliveryInProgress:
		if delivery.Status.StartedAt != nil {
			remaining := deliveryClaimTimeout - now.Sub(delivery.Status.StartedAt.Time)
			if remaining > 0 {
				return ctrl.Result{RequeueAfter: remaining}, nil
			}
		}
		if delivery.Spec.DeliveryPolicy == deliveryPolicyAtMostOnce {
			return ctrl.Result{}, d.completeDelivery(ctx, &delivery, delivery.Status.ActiveAttemptID,
				v1alpha1.NotificationDeliveryAmbiguous, DeliveryResponse{}, errors.New(unknownDeliveryOutcome), time.Time{})
		}
		if err := d.returnDeliveryToPending(ctx, &delivery, delivery.Status.ActiveAttemptID, now); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
	if delivery.Status.NextAttemptAt != nil && now.Before(delivery.Status.NextAttemptAt.Time) {
		return ctrl.Result{RequeueAfter: delivery.Status.NextAttemptAt.Time.Sub(now)}, nil
	}
	if delivery.Status.AttemptCount >= delivery.Spec.MaxAttempts {
		return ctrl.Result{}, d.completeDelivery(ctx, &delivery, delivery.Status.ActiveAttemptID,
			v1alpha1.NotificationDeliveryFailed, DeliveryResponse{}, errors.New(redactedDeliveryFailure), time.Time{})
	}
	var audit v1alpha1.PowerAuditEvent
	if err := d.secretReader.Get(ctx, client.ObjectKey{Namespace: delivery.Namespace, Name: delivery.Spec.AuditEvent.Name}, &audit); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, d.completeDelivery(ctx, &delivery, delivery.Status.ActiveAttemptID,
				v1alpha1.NotificationDeliveryFailed, DeliveryResponse{}, errors.New("source audit event no longer exists"), time.Time{})
		}
		return d.deferOrFail(ctx, &delivery, DeliveryResponse{}, fmt.Errorf("read source audit event: %w", err), false)
	}
	if string(audit.UID) != delivery.Spec.AuditEvent.UID && !(audit.UID == "" && strings.HasPrefix(delivery.Spec.AuditEvent.UID, "name:")) {
		return ctrl.Result{}, d.completeDelivery(ctx, &delivery, delivery.Status.ActiveAttemptID,
			v1alpha1.NotificationDeliveryFailed, DeliveryResponse{}, errors.New("source audit event identity changed"), time.Time{})
	}
	if audit.Spec.NotificationSuppressed || !deliveryMatchesAudit(&delivery, &audit) {
		return ctrl.Result{}, d.completeDelivery(ctx, &delivery, delivery.Status.ActiveAttemptID,
			v1alpha1.NotificationDeliveryFailed, DeliveryResponse{}, errors.New("source audit event does not authorize this delivery"), time.Time{})
	}

	var channel v1alpha1.PowerNotificationChannel
	if err := d.secretReader.Get(ctx, client.ObjectKey{Namespace: delivery.Namespace, Name: delivery.Spec.Channel.Name}, &channel); err != nil {
		return d.deferOrFail(ctx, &delivery, DeliveryResponse{}, fmt.Errorf("read notification channel: %w", err), false)
	}
	if string(channel.UID) != delivery.Spec.Channel.UID && !(channel.UID == "" && strings.HasPrefix(delivery.Spec.Channel.UID, "name:")) {
		return ctrl.Result{}, d.completeDelivery(ctx, &delivery, delivery.Status.ActiveAttemptID,
			v1alpha1.NotificationDeliveryFailed, DeliveryResponse{}, errors.New("notification channel identity changed"), time.Time{})
	}
	if !channel.Spec.Enabled {
		return ctrl.Result{}, d.completeDelivery(ctx, &delivery, delivery.Status.ActiveAttemptID,
			v1alpha1.NotificationDeliveryFailed, DeliveryResponse{}, errors.New("notification channel disabled before delivery"), time.Time{})
	}
	head, err := d.deliveryIsThrottleHead(ctx, &delivery)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !head {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if delay, err := d.deliveryThrottleDelay(ctx, &delivery, &channel, now); err != nil {
		return ctrl.Result{}, err
	} else if delay > 0 {
		next := metav1.NewTime(now.Add(delay))
		_, err := d.transitionDeliveryStatus(ctx, client.ObjectKeyFromObject(&delivery), func(status *v1alpha1.PowerNotificationDeliveryStatus) bool {
			if (status.Phase != "" && status.Phase != v1alpha1.NotificationDeliveryPending) || status.ActiveAttemptID != "" {
				return false
			}
			status.Phase = v1alpha1.NotificationDeliveryPending
			status.NextAttemptAt = &next
			return true
		})
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: delay}, nil
	}
	url, err := d.resolveWebhookURL(ctx, &channel)
	if err != nil {
		return d.deferOrFail(ctx, &delivery, DeliveryResponse{}, err, false)
	}
	sender, ok := d.senders[channel.Spec.Type]
	if !ok {
		return d.deferOrFail(ctx, &delivery, DeliveryResponse{}, fmt.Errorf("unsupported provider type %q", channel.Spec.Type), false)
	}

	attemptID, err := d.claimDelivery(ctx, &delivery, now)
	if err != nil {
		return ctrl.Result{}, err
	}
	payload := eventFromDelivery(&delivery, attemptID)
	response := DeliveryResponse{Attempts: 1}
	if observable, ok := sender.(observableSender); ok {
		response, err = observable.SendWithResult(ctx, url, payload)
	} else {
		err = sender.Send(ctx, url, payload)
	}
	if err == nil {
		return ctrl.Result{}, d.completeDelivery(ctx, &delivery, attemptID,
			v1alpha1.NotificationDeliverySucceeded, response, nil, time.Time{})
	}
	return d.deferOrFailClaim(ctx, &delivery, attemptID, response, err)
}

func (d *Dispatcher) deliveryThrottleDelay(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery, channel *v1alpha1.PowerNotificationChannel, now time.Time) (time.Duration, error) {
	duration := 5 * time.Minute
	if channel.Spec.Throttle != "" {
		if parsed, err := time.ParseDuration(channel.Spec.Throttle); err == nil && parsed > 0 {
			duration = parsed
		}
	}
	key := delivery.Labels["power.aura.sh/throttle-key"]
	if key == "" {
		return 0, nil
	}
	var deliveries v1alpha1.PowerNotificationDeliveryList
	if err := d.secretReader.List(ctx, &deliveries, client.InNamespace(delivery.Namespace), client.MatchingLabels{
		"power.aura.sh/channel":      delivery.Labels["power.aura.sh/channel"],
		"power.aura.sh/throttle-key": key,
	}); err != nil {
		return 0, fmt.Errorf("list prior deliveries for throttle: %w", err)
	}
	var latest time.Time
	for i := range deliveries.Items {
		candidate := &deliveries.Items[i]
		if candidate.Name == delivery.Name || candidate.Spec.Channel.UID != delivery.Spec.Channel.UID || candidate.Status.Phase != v1alpha1.NotificationDeliverySucceeded || candidate.Status.CompletedAt == nil {
			continue
		}
		if candidate.Status.CompletedAt.Time.After(latest) {
			latest = candidate.Status.CompletedAt.Time
		}
	}
	if latest.IsZero() {
		return 0, nil
	}
	expires := latest.Add(duration)
	if now.Before(expires) {
		return expires.Sub(now), nil
	}
	return 0, nil
}

// deliveryIsThrottleHead serializes deliveries for one channel/event key. The
// ordering is deterministic, so concurrent reconcilers independently choose
// the same head without an in-memory or lease-based lock.
func (d *Dispatcher) deliveryIsThrottleHead(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery) (bool, error) {
	key := delivery.Labels["power.aura.sh/throttle-key"]
	channel := delivery.Labels["power.aura.sh/channel"]
	if key == "" || channel == "" {
		return true, nil
	}
	var deliveries v1alpha1.PowerNotificationDeliveryList
	if err := d.secretReader.List(ctx, &deliveries, client.InNamespace(delivery.Namespace), client.MatchingLabels{
		"power.aura.sh/channel": channel, "power.aura.sh/throttle-key": key,
	}); err != nil {
		return false, fmt.Errorf("list deliveries for throttle ordering: %w", err)
	}
	head := delivery
	for i := range deliveries.Items {
		candidate := &deliveries.Items[i]
		if candidate.Spec.Channel.UID != delivery.Spec.Channel.UID || isTerminalDelivery(candidate.Status.Phase) {
			continue
		}
		if candidate.CreationTimestamp.Time.Before(head.CreationTimestamp.Time) ||
			(candidate.CreationTimestamp.Time.Equal(head.CreationTimestamp.Time) && candidate.Name < head.Name) {
			head = candidate
		}
	}
	return head.Name == delivery.Name, nil
}

func isTerminalDelivery(phase string) bool {
	return phase == v1alpha1.NotificationDeliverySucceeded || phase == v1alpha1.NotificationDeliveryFailed || phase == v1alpha1.NotificationDeliveryAmbiguous
}

func deliveryMatchesAudit(delivery *v1alpha1.PowerNotificationDelivery, audit *v1alpha1.PowerAuditEvent) bool {
	want := v1alpha1.NotificationEventSnapshot{
		Action: audit.Spec.Action, Target: audit.Spec.Target, Result: audit.Spec.Result,
		Reason: audit.Spec.Reason, RuleName: audit.Spec.RuleName,
		Timestamp: metav1.NewTime(audit.Spec.Timestamp.Time.UTC().Truncate(time.Second)),
	}
	got := delivery.Spec.Event
	if !got.Timestamp.Time.Equal(want.Timestamp.Time) {
		return false
	}
	got.Timestamp = metav1.Time{}
	want.Timestamp = metav1.Time{}
	return equality.Semantic.DeepEqual(got, want)
}

// reconcileOutbox keeps Run useful for embedders that do not use a
// controller-runtime Manager. Normal deployments use SetupWithManager watches.
func (d *Dispatcher) reconcileOutbox(ctx context.Context) error {
	var deliveries v1alpha1.PowerNotificationDeliveryList
	opts := []client.ListOption{}
	if d.controlNamespace != "" {
		opts = append(opts, client.InNamespace(d.controlNamespace))
	}
	if err := d.client.List(ctx, &deliveries, opts...); err != nil {
		return err
	}
	var errs []error
	for i := range deliveries.Items {
		item := &deliveries.Items[i]
		if _, err := d.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(item)}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func eventFromDelivery(delivery *v1alpha1.PowerNotificationDelivery, attemptID string) Event {
	e := delivery.Spec.Event
	auditRef := delivery.Namespace + "/" + delivery.Spec.AuditEvent.Name
	eventDigest := sha256.Sum256([]byte(auditRef))
	return Event{
		AuditEventRef: auditRef,
		AttemptID:     attemptID, IdempotencyKey: delivery.Spec.IdempotencyKey,
		EventIDs: []string{fmt.Sprintf("event-%x", eventDigest[:16])}, AuditEventRefs: []string{auditRef},
		Action: e.Action, Result: e.Result, Reason: e.Reason, RuleName: e.RuleName, Timestamp: e.Timestamp.Time,
		Target: TargetRef{Namespace: e.Target.Namespace, Name: e.Target.Name, Kind: e.Target.Kind, UID: e.Target.UID},
	}
}

func newAttemptID(delivery *v1alpha1.PowerNotificationDelivery) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d-%s", delivery.Name, delivery.Status.AttemptCount+1, hex.EncodeToString(random)), nil
}

func (d *Dispatcher) claimDelivery(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery, now time.Time) (string, error) {
	attemptID, err := newAttemptID(delivery)
	if err != nil {
		return "", fmt.Errorf("create delivery attempt identity: %w", err)
	}
	key := client.ObjectKeyFromObject(delivery)
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var current v1alpha1.PowerNotificationDelivery
		if err := d.client.Get(ctx, key, &current); err != nil {
			return err
		}
		if current.Status.Phase != "" && current.Status.Phase != v1alpha1.NotificationDeliveryPending {
			return apierrors.NewConflict(v1alpha1.GroupVersion.WithResource("powernotificationdeliveries").GroupResource(), current.Name, errors.New("delivery is no longer pending"))
		}
		started := metav1.NewTime(now)
		current.Status.Phase = v1alpha1.NotificationDeliveryInProgress
		current.Status.StartedAt = &started
		current.Status.CompletedAt = nil
		current.Status.NextAttemptAt = nil
		current.Status.ActiveAttemptID = attemptID
		current.Status.AttemptCount++
		current.Status.ObservedGeneration = current.Generation
		if err := d.persistDeliveryStatus(ctx, &current); err != nil {
			return err
		}
		*delivery = current
		return nil
	})
	return attemptID, err
}

func (d *Dispatcher) returnDeliveryToPending(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery, attemptID string, now time.Time) error {
	_, err := d.transitionDeliveryStatus(ctx, client.ObjectKeyFromObject(delivery), func(status *v1alpha1.PowerNotificationDeliveryStatus) bool {
		if status.Phase != v1alpha1.NotificationDeliveryInProgress || status.ActiveAttemptID != attemptID || attemptID == "" {
			return false
		}
		status.Phase = v1alpha1.NotificationDeliveryPending
		status.ActiveAttemptID = ""
		status.StartedAt = nil
		next := metav1.NewTime(now)
		status.NextAttemptAt = &next
		return true
	})
	return err
}

func retryDelay(attempt int32) time.Duration {
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 6 {
		shift = 6
	}
	return time.Second * time.Duration(1<<shift)
}

func (d *Dispatcher) deferOrFail(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery, response DeliveryResponse, err error, ambiguous bool) (ctrl.Result, error) {
	// Pre-send failures are safe to retry under either policy because the
	// provider request has not started. Keep these records recoverable: a Secret,
	// channel, API server, or upgraded sender can become available later without
	// consuming the external side-effect budget.
	next := time.Now().UTC().Add(retryDelay(delivery.Status.PreflightFailureCount + 1))
	if updateErr := d.completeDelivery(ctx, delivery, "", v1alpha1.NotificationDeliveryPending, response, err, next); updateErr != nil {
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{RequeueAfter: time.Until(next)}, nil
}

func (d *Dispatcher) deferOrFailClaim(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery, attemptID string, response DeliveryResponse, err error) (ctrl.Result, error) {
	ambiguous := response.StatusCode == 0
	if delivery.Spec.DeliveryPolicy == deliveryPolicyAtLeastOnce && delivery.Status.AttemptCount < delivery.Spec.MaxAttempts {
		next := time.Now().UTC().Add(retryDelay(delivery.Status.AttemptCount))
		if updateErr := d.completeDelivery(ctx, delivery, attemptID, v1alpha1.NotificationDeliveryPending, response, err, next); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{RequeueAfter: time.Until(next)}, nil
	}
	phase := v1alpha1.NotificationDeliveryFailed
	if ambiguous {
		phase = v1alpha1.NotificationDeliveryAmbiguous
	}
	return ctrl.Result{}, d.completeDelivery(ctx, delivery, attemptID, phase, response, err, time.Time{})
}

func (d *Dispatcher) completeDelivery(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery, attemptID, phase string, response DeliveryResponse, deliveryErr error, next time.Time) error {
	key := client.ObjectKeyFromObject(delivery)
	now := metav1.Now()
	safeErr := redactDeliveryError(deliveryErr, "")
	claimed := attemptID != ""
	recordedAttemptID := attemptID
	if recordedAttemptID == "" && phase != v1alpha1.NotificationDeliveryPending {
		recordedAttemptID = delivery.Spec.IdempotencyKey + "-preflight"
	}
	applied := false
	_, err := d.transitionDeliveryStatus(ctx, key, func(status *v1alpha1.PowerNotificationDeliveryStatus) bool {
		if isTerminalDelivery(status.Phase) {
			return false
		}
		if claimed {
			if status.Phase != v1alpha1.NotificationDeliveryInProgress || status.ActiveAttemptID != attemptID {
				return false
			}
		} else if (status.Phase != "" && status.Phase != v1alpha1.NotificationDeliveryPending) || status.ActiveAttemptID != "" {
			return false
		}
		applied = true
		status.Phase = phase
		status.ProviderStatusCode = int32(response.StatusCode)
		status.ActiveAttemptID = ""
		if recordedAttemptID != "" {
			status.LastAttemptID = recordedAttemptID
		}
		if phase == v1alpha1.NotificationDeliveryPending {
			if claimed {
				status.RetryCount++
			} else {
				if status.PreflightFailureCount < int32(^uint32(0)>>1) {
					status.PreflightFailureCount++
				}
			}
			status.StartedAt = nil
			at := metav1.NewTime(next)
			status.NextAttemptAt = &at
			status.Response = safeErr.Error()
			return true
		}
		status.CompletedAt = &now
		status.NextAttemptAt = nil
		if deliveryErr == nil {
			status.Response = "accepted"
		} else {
			status.Response = safeErr.Error()
		}
		return true
	})
	if err != nil || !applied {
		return err
	}
	if phase == v1alpha1.NotificationDeliverySucceeded || phase == v1alpha1.NotificationDeliveryFailed || phase == v1alpha1.NotificationDeliveryAmbiguous {
		return d.ensureChannelStatus(ctx, delivery)
	}
	return nil
}

func (d *Dispatcher) ensureChannelStatus(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery) error {
	var current v1alpha1.PowerNotificationDelivery
	if err := d.client.Get(ctx, client.ObjectKeyFromObject(delivery), &current); err != nil {
		return client.IgnoreNotFound(err)
	}
	if current.Status.ChannelStatusRecorded {
		return nil
	}
	if current.Status.Phase != v1alpha1.NotificationDeliverySucceeded && current.Status.Phase != v1alpha1.NotificationDeliveryFailed && current.Status.Phase != v1alpha1.NotificationDeliveryAmbiguous {
		return errors.New("refusing to project a non-terminal notification delivery")
	}
	if current.Status.LastAttemptID == "" {
		return errors.New("terminal notification delivery lacks an attempt identity")
	}
	var channel v1alpha1.PowerNotificationChannel
	err := d.secretReader.Get(ctx, client.ObjectKey{Namespace: current.Namespace, Name: current.Spec.Channel.Name}, &channel)
	if err == nil && (string(channel.UID) == current.Spec.Channel.UID || (channel.UID == "" && strings.HasPrefix(current.Spec.Channel.UID, "name:"))) {
		var deliveryErr error
		if current.Status.Phase != v1alpha1.NotificationDeliverySucceeded {
			deliveryErr = errors.New(current.Status.Response)
		}
		if err := d.mirrorDeliveryToChannel(ctx, &current, current.Status.LastAttemptID, current.Status.Phase,
			DeliveryResponse{StatusCode: int(current.Status.ProviderStatusCode), Attempts: int(current.Status.AttemptCount)}, deliveryErr); err != nil {
			return err
		}
	} else if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	_, err = d.transitionDeliveryStatus(ctx, client.ObjectKeyFromObject(&current), func(status *v1alpha1.PowerNotificationDeliveryStatus) bool {
		if status.Phase == current.Status.Phase && status.LastAttemptID == current.Status.LastAttemptID {
			status.ChannelStatusRecorded = true
			return true
		}
		return false
	})
	return err
}

// transitionDeliveryStatus combines optimistic resource-version concurrency
// with a semantic compare-and-swap supplied by each state transition.
func (d *Dispatcher) transitionDeliveryStatus(ctx context.Context, key types.NamespacedName, mutate func(*v1alpha1.PowerNotificationDeliveryStatus) bool) (bool, error) {
	backoff := wait.Backoff{Steps: 5, Duration: 20 * time.Millisecond, Factor: 2, Jitter: 0.1}
	applied := false
	err := retry.OnError(backoff, func(error) bool { return ctx.Err() == nil }, func() error {
		var current v1alpha1.PowerNotificationDelivery
		if err := d.client.Get(ctx, key, &current); err != nil {
			return err
		}
		if !mutate(&current.Status) {
			return nil
		}
		applied = true
		return d.persistDeliveryStatus(ctx, &current)
	})
	return applied, err
}

func (d *Dispatcher) persistDeliveryStatus(ctx context.Context, current *v1alpha1.PowerNotificationDelivery) error {
	err := d.client.Status().Update(ctx, current)
	if !apierrors.IsNotFound(err) {
		return err
	}
	// controller-runtime's fake client requires explicit status registration.
	// A live API cannot have the object while its status endpoint is NotFound.
	var exists v1alpha1.PowerNotificationDelivery
	if getErr := d.client.Get(ctx, client.ObjectKeyFromObject(current), &exists); getErr != nil {
		return err
	}
	exists.Status = current.Status
	return d.client.Update(ctx, &exists)
}

func (d *Dispatcher) mirrorDeliveryToChannel(ctx context.Context, delivery *v1alpha1.PowerNotificationDelivery, attemptID, phase string, response DeliveryResponse, safeErr error) error {
	if attemptID == "" {
		return errors.New("refusing to record a notification attempt without identity")
	}
	event := eventFromDelivery(delivery, attemptID)
	eventIDs, refs := correlatedEventIDs([]Event{event})
	started := metav1.Now()
	if delivery.Status.StartedAt != nil {
		started = *delivery.Status.StartedAt.DeepCopy()
	}
	attempt := &v1alpha1.NotificationAttemptStatus{
		ID: attemptID, EventIDs: eventIDs, AuditEventRefs: refs, Phase: phase,
		StartedAt: started, AttemptCount: delivery.Status.AttemptCount, ProviderStatusCode: int32(response.StatusCode),
	}
	if phase == v1alpha1.NotificationDeliverySucceeded {
		attempt.Phase = "Succeeded"
		attempt.Response = "accepted"
	} else if phase == v1alpha1.NotificationDeliveryAmbiguous {
		attempt.Phase = "Ambiguous"
		attempt.Response = safeErr.Error()
	} else {
		attempt.Phase = "Failed"
		attempt.Response = safeErr.Error()
	}
	now := metav1.Now()
	attempt.CompletedAt = &now
	return d.updateChannelStatus(ctx, client.ObjectKey{Namespace: delivery.Namespace, Name: delivery.Spec.Channel.Name}, func(status *v1alpha1.PowerNotificationChannelStatus) {
		if status.LastAttempt != nil && status.LastAttempt.ID == attempt.ID && status.LastAttempt.Phase != "InProgress" {
			return
		}
		storeAttempt(status, attempt)
		if phase == v1alpha1.NotificationDeliverySucceeded {
			status.TotalSent++
			status.LastNotification = &now
			status.LastError = ""
		} else {
			status.TotalErrors++
			status.LastError = safeErr.Error()
		}
	})
}
