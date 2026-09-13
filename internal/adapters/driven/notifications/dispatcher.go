package notifications

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

// Event represents a power event to notify about.
type Event struct {
	// AuditEventRef identifies the persisted PowerAuditEvent that produced this
	// notification (namespace/name). It is safe to expose and contains no payload.
	AuditEventRef string
	// AttemptID, EventIDs, and AuditEventRefs are populated by the dispatcher
	// immediately before delivery so receivers can correlate the request with
	// the channel status without receiving credentials or response bodies.
	AttemptID      string
	EventIDs       []string
	AuditEventRefs []string
	Action         string
	Target         TargetRef
	Result         string
	Reason         string
	RuleName       string
	Timestamp      time.Time
}

// TargetRef identifies the affected workload.
type TargetRef struct {
	Namespace string
	Name      string
	Kind      string
	UID       string
}

// Dispatcher sends notifications to configured channels.
type Dispatcher struct {
	client       client.Client
	secretReader client.Reader
	senders      map[string]Sender
	queue        chan Event
	mu           sync.Mutex
	throttle     map[string]time.Time // key: "channel/target" → expiry
}

// Sender formats and sends a notification for a given provider type.
type Sender interface {
	Send(ctx context.Context, url string, event Event) error
	Type() string
}

type observableSender interface {
	SendWithResult(ctx context.Context, url string, event Event) (DeliveryResponse, error)
}

// NewDispatcher creates a notification dispatcher. secretReader must bypass
// the manager cache: watching Secrets cluster-wide would require broader RBAC
// than the namespace-scoped get permission used by notification channels.
func NewDispatcher(c client.Client, secretReader client.Reader) *Dispatcher {
	d := &Dispatcher{
		client:       c,
		secretReader: secretReader,
		senders:      make(map[string]Sender),
		queue:        make(chan Event, 500),
		throttle:     make(map[string]time.Time),
	}
	// Register built-in senders
	d.RegisterSender(&GoogleChatSender{})
	d.RegisterSender(&GenericSender{})
	d.RegisterSender(&SlackSender{})
	return d
}

// RegisterSender adds a sender for a provider type.
func (d *Dispatcher) RegisterSender(s Sender) {
	d.senders[s.Type()] = s
}

var ErrQueueFull = errors.New("notification queue is full")

// Enqueue adds an event to the notification queue. It never evicts another
// event: the caller owns the durable audit record and must retry when capacity
// is unavailable.
func (d *Dispatcher) Enqueue(event Event) error {
	select {
	case d.queue <- event:
		return nil
	default:
		return ErrQueueFull
	}
}

// Start runs the dispatcher under the controller manager after cache readiness.
// Only the elected leader dispatches, preventing duplicate deliveries when the
// controller has multiple replicas.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.Run(ctx)
	return nil
}

// NeedLeaderElection makes Dispatcher a leader-only manager runnable.
func (d *Dispatcher) NeedLeaderElection() bool { return true }

// Run starts the dispatcher loop and blocks until context is cancelled. It
// batches events received within five seconds into one delivery per channel.
func (d *Dispatcher) Run(ctx context.Context) {
	log := ctrl.Log.WithName("notifications")
	log.Info("notification dispatcher started (batch mode: 5s window)")
	// Any in-progress record visible before this process starts belongs to a
	// delivery whose outcome cannot safely be inferred. Finalize it as unknown
	// without replaying the webhook, which could duplicate an accepted request.
	if err := d.recoverIncompleteAttempts(ctx, time.Now()); err != nil {
		log.Error(err, "failed to recover incomplete notification attempts")
	}
	recoveryTicker := time.NewTicker(30 * time.Second)
	defer recoveryTicker.Stop()

	for {
		// Wait for first event or context cancel
		select {
		case <-ctx.Done():
			log.Info("notification dispatcher stopped")
			return
		case <-recoveryTicker.C:
			// This also recovers a final status write that remained unavailable
			// without a process restart. The sender timeout is at most 19 seconds,
			// so a minute-old attempt cannot still be executing normally.
			if err := d.recoverIncompleteAttempts(ctx, time.Now().Add(-time.Minute)); err != nil {
				log.Error(err, "failed to recover stale notification attempts")
			}
		case first := <-d.queue:
			// Collect events for 5 seconds into a batch
			batch := []Event{first}
			timer := time.NewTimer(5 * time.Second)
		batchLoop:
			for {
				select {
				case ev := <-d.queue:
					batch = append(batch, ev)
				case <-timer.C:
					break batchLoop
				case <-ctx.Done():
					timer.Stop()
					return
				}
			}
			if err := d.dispatchBatch(ctx, batch); err != nil {
				log.Error(err, "notification batch completed with persistence errors")
			}
		}
	}
}

const unknownDeliveryOutcome = "delivery outcome unknown after dispatcher interruption; automatic redelivery suppressed"
const redactedDeliveryFailure = "notification delivery failed; endpoint and transport details <redacted>"

// recoverIncompleteAttempts makes an interrupted delivery visible without
// guessing whether the provider accepted it. It deliberately never calls a
// Sender: an operator or a future source event may retry, but recovery cannot
// safely replay an ambiguous side effect.
func (d *Dispatcher) recoverIncompleteAttempts(ctx context.Context, startedBefore time.Time) error {
	var channels v1alpha1.PowerNotificationChannelList
	if err := d.client.List(ctx, &channels); err != nil {
		return fmt.Errorf("list notification channels for recovery: %w", err)
	}
	var recoveryErrs []error
	for i := range channels.Items {
		channel := &channels.Items[i]
		hasRecoverable := false
		for j := range channel.Status.RecentAttempts {
			attempt := &channel.Status.RecentAttempts[j]
			if attempt.Phase == "InProgress" && !attempt.StartedAt.Time.After(startedBefore) {
				hasRecoverable = true
				break
			}
		}
		if channel.Status.LastAttempt != nil && channel.Status.LastAttempt.Phase == "InProgress" &&
			!channel.Status.LastAttempt.StartedAt.Time.After(startedBefore) {
			hasRecoverable = true
		}
		if !hasRecoverable {
			continue
		}
		err := d.updateChannelStatus(ctx, client.ObjectKeyFromObject(channel), func(status *v1alpha1.PowerNotificationChannelStatus) {
			now := metav1.Now()
			recovered := make(map[string]struct{})
			for j := range status.RecentAttempts {
				attempt := &status.RecentAttempts[j]
				if attempt.Phase != "InProgress" || attempt.StartedAt.Time.After(startedBefore) {
					continue
				}
				attempt.Phase = "Failed"
				attempt.CompletedAt = &now
				attempt.Response = unknownDeliveryOutcome
				status.TotalErrors++
				recovered[attempt.ID] = struct{}{}
			}
			if status.LastAttempt != nil && status.LastAttempt.Phase == "InProgress" &&
				!status.LastAttempt.StartedAt.Time.After(startedBefore) {
				if _, ok := recovered[status.LastAttempt.ID]; !ok {
					status.TotalErrors++
				}
				status.LastAttempt.Phase = "Failed"
				status.LastAttempt.CompletedAt = &now
				status.LastAttempt.Response = unknownDeliveryOutcome
			}
			status.LastError = unknownDeliveryOutcome
		})
		if err != nil {
			recoveryErrs = append(recoveryErrs, fmt.Errorf("recover notification channel %s/%s: %w", channel.Namespace, channel.Name, err))
		}
	}
	return errors.Join(recoveryErrs...)
}

func (d *Dispatcher) dispatchBatch(ctx context.Context, batch []Event) error {
	log := ctrl.Log.WithName("notifications")
	d.pruneThrottle(time.Now())

	if len(batch) == 0 {
		return nil
	}

	// Filter only notifiable actions and deduplicate
	var filtered []Event
	seen := map[string]bool{}
	for _, ev := range batch {
		key := eventDeduplicationIdentity(ev)
		if seen[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, ev)
	}

	if len(filtered) == 0 {
		return nil
	}

	// Load all channels
	var channels v1alpha1.PowerNotificationChannelList
	if err := d.client.List(ctx, &channels); err != nil {
		return fmt.Errorf("list notification channels: %w", err)
	}
	var dispatchErrs []error

	for i := range channels.Items {
		ch := &channels.Items[i]
		if !ch.Spec.Enabled {
			continue
		}

		// Filter events for this channel
		var channelEvents []Event
		for _, ev := range filtered {
			if len(ch.Spec.Events) > 0 && !contains(ch.Spec.Events, ev.Action) {
				continue
			}
			if len(ch.Spec.NamespaceFilter) > 0 && !contains(ch.Spec.NamespaceFilter, ev.Target.Namespace) {
				continue
			}
			channelEvents = append(channelEvents, ev)
		}

		if len(channelEvents) == 0 {
			continue
		}
		channelEvents = excludeAttemptedEvents(ch.Status, channelEvents)
		if len(channelEvents) == 0 {
			continue
		}

		// A message must have one truthful action, result, and rule. Combining
		// different decisions under the first event's metadata makes the webhook
		// claim that unrelated workloads had the same outcome.
		groups := groupBatchEvents(channelEvents)

		// Throttle each semantically homogeneous group independently.
		throttleDur := 5 * time.Minute
		if ch.Spec.Throttle != "" {
			if parsed, err := time.ParseDuration(ch.Spec.Throttle); err == nil && parsed > 0 {
				throttleDur = parsed
			}
		}

		// Resolve URL
		url, err := d.resolveWebhookURL(ctx, ch)
		if err != nil {
			for _, group := range groups {
				dispatchErrs = append(dispatchErrs, d.recordUndelivered(ctx, ch, group, "resolve webhook URL", err, ""))
			}
			continue
		}

		// Find sender
		sender, ok := d.senders[ch.Spec.Type]
		if !ok {
			for _, group := range groups {
				dispatchErrs = append(dispatchErrs, d.recordUndelivered(ctx, ch, group, "select notification sender", fmt.Errorf("unsupported provider type %q", ch.Spec.Type), ""))
			}
			continue
		}

		for _, group := range groups {
			key := batchThrottleKey(ch, group)
			d.mu.Lock()
			expiresAt, exists := d.throttle[key]
			d.mu.Unlock()
			if exists && time.Now().Before(expiresAt) {
				continue
			}

			batchEvent := summarizeBatch(group)
			delivered, err := d.deliver(ctx, ch, group, batchEvent, sender, url)
			if delivered {
				d.mu.Lock()
				d.throttle[key] = time.Now().Add(throttleDur)
				d.mu.Unlock()
			}
			if err != nil {
				dispatchErrs = append(dispatchErrs, err)
				continue
			}
			log.Info("batch notification sent", "channel", ch.Name, "events", len(group), "action", batchEvent.Action, "result", batchEvent.Result, "rule", batchEvent.RuleName)
		}
	}
	return errors.Join(dispatchErrs...)
}

func groupBatchEvents(events []Event) [][]Event {
	type semanticKey struct {
		action string
		result string
		rule   string
	}
	groups := make([][]Event, 0)
	indexes := make(map[semanticKey]int)
	for _, event := range events {
		key := semanticKey{action: event.Action, result: event.Result, rule: event.RuleName}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], event)
	}
	return groups
}

func excludeAttemptedEvents(status v1alpha1.PowerNotificationChannelStatus, events []Event) []Event {
	attempted := make(map[string]struct{})
	for i := range status.RecentAttempts {
		for _, ref := range status.RecentAttempts[i].AuditEventRefs {
			attempted[ref] = struct{}{}
		}
	}
	if status.LastAttempt != nil {
		for _, ref := range status.LastAttempt.AuditEventRefs {
			attempted[ref] = struct{}{}
		}
	}
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if event.AuditEventRef != "" {
			if _, exists := attempted[event.AuditEventRef]; exists {
				continue
			}
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func summarizeBatch(events []Event) Event {
	batchEvent := events[0]
	if len(events) == 1 {
		return batchEvent
	}

	names := fmt.Sprintf("%d workload(s): ", len(events))
	for i, event := range events {
		if i > 4 {
			names += fmt.Sprintf(" (+%d more)", len(events)-5)
			break
		}
		if i > 0 {
			names += ", "
		}
		names += fmt.Sprintf("%s/%s (%s)", event.Target.Namespace, event.Target.Name, event.Target.Kind)
	}
	batchEvent.Reason = names
	return batchEvent
}

func (d *Dispatcher) dispatch(ctx context.Context, event Event) error {
	log := ctrl.Log.WithName("notifications")
	d.pruneThrottle(time.Now())

	// Load all channels
	var channels v1alpha1.PowerNotificationChannelList
	if err := d.client.List(ctx, &channels); err != nil {
		return fmt.Errorf("list notification channels: %w", err)
	}
	var dispatchErrs []error

	for i := range channels.Items {
		ch := &channels.Items[i]
		if !ch.Spec.Enabled {
			continue
		}

		// Event filter
		if len(ch.Spec.Events) > 0 && !contains(ch.Spec.Events, event.Action) {
			continue
		}

		// Namespace filter
		if len(ch.Spec.NamespaceFilter) > 0 && !contains(ch.Spec.NamespaceFilter, event.Target.Namespace) {
			continue
		}
		if len(excludeAttemptedEvents(ch.Status, []Event{event})) == 0 {
			continue
		}

		// Throttle check (default: 5m per target even if not configured)
		throttleDur := 5 * time.Minute
		if ch.Spec.Throttle != "" {
			if parsed, err := time.ParseDuration(ch.Spec.Throttle); err == nil && parsed > 0 {
				throttleDur = parsed
			}
		}
		key := ch.Name + "/" + eventIdentity(event)
		d.mu.Lock()
		expiresAt, exists := d.throttle[key]
		if exists && time.Now().Before(expiresAt) {
			d.mu.Unlock()
			continue
		}
		d.mu.Unlock()

		// Resolve URL
		url, err := d.resolveWebhookURL(ctx, ch)
		if err != nil {
			dispatchErrs = append(dispatchErrs, d.recordUndelivered(ctx, ch, []Event{event}, "resolve webhook URL", err, ""))
			continue
		}

		// Find sender
		sender, ok := d.senders[ch.Spec.Type]
		if !ok {
			dispatchErrs = append(dispatchErrs, d.recordUndelivered(ctx, ch, []Event{event}, "select notification sender", fmt.Errorf("unsupported provider type %q", ch.Spec.Type), ""))
			continue
		}

		delivered, err := d.deliver(ctx, ch, []Event{event}, event, sender, url)
		if delivered {
			d.mu.Lock()
			d.throttle[key] = time.Now().Add(throttleDur)
			d.mu.Unlock()
		}
		if err != nil {
			dispatchErrs = append(dispatchErrs, err)
			continue
		}
		log.Info("notification sent", "channel", ch.Name, "type", ch.Spec.Type, "target", event.Target.Name)
	}
	return errors.Join(dispatchErrs...)
}

func (d *Dispatcher) pruneThrottle(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, expiresAt := range d.throttle {
		if !now.Before(expiresAt) {
			delete(d.throttle, key)
		}
	}
}

func (d *Dispatcher) resolveWebhookURL(ctx context.Context, channel *v1alpha1.PowerNotificationChannel) (string, error) {
	if channel.Spec.URL != "" {
		return validateWebhookURL(channel.Spec.URL)
	}
	if channel.Spec.URLFrom == nil {
		return "", fmt.Errorf("channel has neither url nor urlFrom configured")
	}

	var secret corev1.Secret
	key := client.ObjectKey{Namespace: channel.Namespace, Name: channel.Spec.URLFrom.Name}
	if err := d.secretReader.Get(ctx, key, &secret); err != nil {
		return "", fmt.Errorf("read referenced Secret %s/%s: %w", key.Namespace, key.Name, err)
	}
	value, ok := secret.Data[channel.Spec.URLFrom.Key]
	if !ok {
		return "", fmt.Errorf("referenced Secret %s/%s does not contain key %q", key.Namespace, key.Name, channel.Spec.URLFrom.Key)
	}
	url := strings.TrimSpace(string(value))
	if url == "" {
		return "", fmt.Errorf("referenced Secret %s/%s contains an empty key %q", key.Namespace, key.Name, channel.Spec.URLFrom.Key)
	}
	return validateWebhookURL(url)
}

func validateWebhookURL(value string) (string, error) {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", errors.New("webhook endpoint must be an absolute HTTP(S) URL without userinfo; endpoint details <redacted>")
	}
	return value, nil
}

func (d *Dispatcher) recordUndelivered(ctx context.Context, channel *v1alpha1.PowerNotificationChannel, events []Event, operation string, err error, secretURL string) error {
	attempt, persistErr := d.beginAttempt(ctx, channel, events)
	if persistErr != nil {
		return fmt.Errorf("%s: persist notification attempt before delivery: %w", operation, persistErr)
	}
	safeErr := redactDeliveryError(err, secretURL)
	ctrl.Log.WithName("notifications").Error(safeErr, "notification delivery failed",
		"operation", operation, "channel", channel.Name, "namespace", channel.Namespace, "type", channel.Spec.Type)
	if updateErr := d.finishAttempt(ctx, channel, attempt, DeliveryResponse{}, safeErr); updateErr != nil {
		return errors.Join(safeErr, fmt.Errorf("persist notification failure: %w", updateErr))
	}
	return safeErr
}

func (d *Dispatcher) deliver(ctx context.Context, channel *v1alpha1.PowerNotificationChannel, events []Event, payload Event, sender Sender, url string) (bool, error) {
	attempt, err := d.beginAttempt(ctx, channel, events)
	if err != nil {
		return false, fmt.Errorf("persist notification attempt before delivery: %w", err)
	}
	response := DeliveryResponse{Attempts: 1}
	payload.AttemptID = attempt.ID
	payload.EventIDs = append([]string(nil), attempt.EventIDs...)
	payload.AuditEventRefs = append([]string(nil), attempt.AuditEventRefs...)
	if observable, ok := sender.(observableSender); ok {
		response, err = observable.SendWithResult(ctx, url, payload)
	} else {
		err = sender.Send(ctx, url, payload)
	}
	if err != nil {
		safeErr := redactDeliveryError(err, url)
		ctrl.Log.WithName("notifications").Error(safeErr, "notification delivery failed",
			"operation", "send notification", "channel", channel.Name, "namespace", channel.Namespace, "type", channel.Spec.Type,
			"attemptID", attempt.ID)
		if updateErr := d.finishAttempt(ctx, channel, attempt, response, safeErr); updateErr != nil {
			return false, errors.Join(safeErr, fmt.Errorf("persist notification failure: %w", updateErr))
		}
		return false, safeErr
	}
	if err := d.finishAttempt(ctx, channel, attempt, response, nil); err != nil {
		return true, fmt.Errorf("notification delivered but success status was not persisted: %w", err)
	}
	return true, nil
}

func (d *Dispatcher) beginAttempt(ctx context.Context, channel *v1alpha1.PowerNotificationChannel, events []Event) (*v1alpha1.NotificationAttemptStatus, error) {
	now := metav1.Now()
	eventIDs, auditRefs := correlatedEventIDs(events)
	seed := fmt.Sprintf("%s/%s\x00%s\x00%d", channel.Namespace, channel.Name, strings.Join(eventIDs, "\n"), now.UnixNano())
	digest := sha256.Sum256([]byte(seed))
	attempt := &v1alpha1.NotificationAttemptStatus{
		ID: fmt.Sprintf("attempt-%x", digest[:16]), EventIDs: eventIDs, AuditEventRefs: auditRefs,
		Phase: "InProgress", StartedAt: now,
	}
	err := d.updateChannelStatus(ctx, client.ObjectKeyFromObject(channel), func(status *v1alpha1.PowerNotificationChannelStatus) {
		storeAttempt(status, attempt)
	})
	return attempt, err
}

func (d *Dispatcher) finishAttempt(ctx context.Context, channel *v1alpha1.PowerNotificationChannel, attempt *v1alpha1.NotificationAttemptStatus, response DeliveryResponse, deliveryErr error) error {
	now := metav1.Now()
	return d.updateChannelStatus(ctx, client.ObjectKeyFromObject(channel), func(status *v1alpha1.PowerNotificationChannelStatus) {
		// An ambiguous API response may have committed a previous retry. Never
		// increment counters twice for the same attempt identifier.
		if status.LastAttempt != nil && status.LastAttempt.ID == attempt.ID && status.LastAttempt.Phase != "InProgress" {
			return
		}
		completed := attempt.DeepCopy()
		completed.CompletedAt = &now
		completed.ProviderStatusCode = int32(response.StatusCode)
		completed.AttemptCount = int32(response.Attempts)
		if deliveryErr == nil {
			completed.Phase = "Succeeded"
			completed.Response = "accepted"
			status.TotalSent++
			status.LastNotification = &now
			status.LastError = ""
		} else {
			completed.Phase = "Failed"
			completed.Response = deliveryErr.Error()
			status.TotalErrors++
			status.LastError = deliveryErr.Error()
		}
		storeAttempt(status, completed)
	})
}

const maxRecentNotificationAttempts = 20

func storeAttempt(status *v1alpha1.PowerNotificationChannelStatus, attempt *v1alpha1.NotificationAttemptStatus) {
	status.LastAttempt = attempt.DeepCopy()
	for i := range status.RecentAttempts {
		if status.RecentAttempts[i].ID == attempt.ID {
			attempt.DeepCopyInto(&status.RecentAttempts[i])
			return
		}
	}
	status.RecentAttempts = append(status.RecentAttempts, *attempt.DeepCopy())
	if overflow := len(status.RecentAttempts) - maxRecentNotificationAttempts; overflow > 0 {
		status.RecentAttempts = append([]v1alpha1.NotificationAttemptStatus(nil), status.RecentAttempts[overflow:]...)
	}
}

func (d *Dispatcher) updateChannelStatus(ctx context.Context, key types.NamespacedName, mutate func(*v1alpha1.PowerNotificationChannelStatus)) error {
	backoff := wait.Backoff{Steps: 5, Duration: 20 * time.Millisecond, Factor: 2, Jitter: 0.1}
	return retry.OnError(backoff, func(error) bool { return ctx.Err() == nil }, func() error {
		var current v1alpha1.PowerNotificationChannel
		if err := d.client.Get(ctx, key, &current); err != nil {
			return err
		}
		mutate(&current.Status)
		return d.client.Status().Update(ctx, &current)
	})
}

func correlatedEventIDs(events []Event) ([]string, []string) {
	ids := make([]string, 0, len(events))
	auditRefs := make([]string, 0, len(events))
	for _, event := range events {
		seed := event.AuditEventRef
		if seed == "" {
			seed = eventIdentity(event) + "/" + event.Timestamp.UTC().Format(time.RFC3339Nano)
		}
		digest := sha256.Sum256([]byte(seed))
		ids = append(ids, fmt.Sprintf("event-%x", digest[:16]))
		if event.AuditEventRef != "" {
			auditRefs = append(auditRefs, event.AuditEventRef)
		}
	}
	sort.Strings(ids)
	sort.Strings(auditRefs)
	return ids, auditRefs
}

func redactDeliveryError(err error, secretURL string) error {
	if err == nil {
		return nil
	}
	// Transport libraries may normalize or escape a URL before returning it,
	// so replacing the original byte string is not a safe redaction boundary.
	// Endpoint values frequently contain credentials even when configured
	// directly. Provider status/attempt counts remain available as structured
	// fields; logs and status receive only this stable classification.
	_ = secretURL
	if errors.Is(err, context.Canceled) {
		return errors.New("notification delivery canceled; endpoint details <redacted>")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("notification delivery timed out; endpoint details <redacted>")
	}
	return errors.New(redactedDeliveryFailure)
}

func batchThrottleKey(channel *v1alpha1.PowerNotificationChannel, events []Event) string {
	identities := make([]string, 0, len(events))
	for _, event := range events {
		identities = append(identities, eventIdentity(event))
	}
	sort.Strings(identities)
	digest := sha256.Sum256([]byte(strings.Join(identities, "\n")))
	return fmt.Sprintf("%s/%s/batch/%x", channel.Namespace, channel.Name, digest[:16])
}

func eventIdentity(event Event) string {
	return event.Action + "/" + event.Result + "/" + event.RuleName + "/" + event.Target.Namespace + "/" + event.Target.Kind + "/" + event.Target.Name + "/" + event.Target.UID
}

func eventDeduplicationIdentity(event Event) string {
	if event.AuditEventRef != "" {
		return eventIdentity(event) + "/" + event.AuditEventRef
	}
	return eventIdentity(event)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
