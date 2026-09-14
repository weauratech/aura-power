package notifications

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func outboxClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.PowerNotificationDelivery{}, &v1alpha1.PowerNotificationChannel{}).
		WithObjects(objects...).Build()
}

func outboxFixture(policy string) (*v1alpha1.PowerAuditEvent, *v1alpha1.PowerNotificationChannel, Event) {
	audit := &v1alpha1.PowerAuditEvent{ObjectMeta: metav1.ObjectMeta{Name: "audit-1", Namespace: "aura-system", UID: types.UID("audit-uid-1")}}
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "channel-1", Namespace: "aura-system", UID: types.UID("channel-uid-1")},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "generic", URL: "https://example.test/hook", Enabled: true, DeliveryPolicy: policy, MaxDeliveryAttempts: 3},
	}
	event := Event{AuditEventRef: "aura-system/audit-1", Action: "execution.error", Result: "error", Reason: "fixture", Timestamp: time.Now().UTC(), Target: TargetRef{Namespace: "workloads", Name: "api", Kind: "Deployment", UID: "target-uid"}}
	audit.Spec = v1alpha1.PowerAuditEventSpec{Action: event.Action, Result: event.Result, Reason: event.Reason, Timestamp: metav1.NewTime(event.Timestamp), Target: v1alpha1.AuditResourceReference{Namespace: event.Target.Namespace, Name: event.Target.Name, Kind: event.Target.Kind, UID: event.Target.UID}}
	return audit, channel, event
}

func TestDurableEnqueueIsPairIdempotentBeyondLegacyHistory(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtMostOnce)
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		copy := audit.DeepCopy()
		copy.Name = "audit-later-" + string(rune('a'+i))
		copy.UID = types.UID("later-uid-" + string(rune('a'+i)))
		copy.ResourceVersion = ""
		if err := c.Create(context.Background(), copy); err != nil {
			t.Fatal(err)
		}
		next := event
		next.AuditEventRef = copy.Namespace + "/" + copy.Name
		if err := d.EnqueueContext(context.Background(), next); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var deliveries v1alpha1.PowerNotificationDeliveryList
	if err := c.List(context.Background(), &deliveries); err != nil {
		t.Fatal(err)
	}
	if len(deliveries.Items) != 26 {
		t.Fatalf("deliveries=%d want=26 unique audit/channel pairs", len(deliveries.Items))
	}
	for _, item := range deliveries.Items {
		if item.Spec.IdempotencyKey == "" || item.Spec.Channel.UID == "" || item.Spec.AuditEvent.UID == "" {
			t.Fatalf("incomplete durable identity: %+v", item.Spec)
		}
	}
}

type committedCreateErrorClient struct {
	client.Client
	once sync.Once
}

type committedStatusErrorClient struct {
	client.Client
	once      sync.Once
	shouldErr func(client.Object) bool
}

func (c *committedStatusErrorClient) Status() client.SubResourceWriter {
	return &committedStatusErrorWriter{SubResourceWriter: c.Client.Status(), parent: c}
}

type committedStatusErrorWriter struct {
	client.SubResourceWriter
	parent *committedStatusErrorClient
}

func (w *committedStatusErrorWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	fault := false
	if w.parent.shouldErr(obj) {
		w.parent.once.Do(func() { fault = true })
	}
	if !fault {
		return w.SubResourceWriter.Update(ctx, obj, opts...)
	}
	if err := w.SubResourceWriter.Update(ctx, obj, opts...); err != nil {
		return err
	}
	return apierrors.NewTimeoutError("status committed but response lost", 1)
}

func (c *committedCreateErrorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*v1alpha1.PowerNotificationDelivery); ok {
		failed := false
		c.once.Do(func() { failed = true })
		if failed {
			if err := c.Client.Create(ctx, obj, opts...); err != nil {
				return err
			}
			return apierrors.NewTimeoutError("committed but response lost", 1)
		}
	}
	return c.Client.Create(ctx, obj, opts...)
}

func TestCommittedButUnobservedOutboxWriteConvergesOnAuditRetry(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtMostOnce)
	base := outboxClient(t, audit, channel)
	c := &committedCreateErrorClient{Client: base}
	d := NewDispatcher(c, c)
	if err := d.EnqueueContext(context.Background(), event); err == nil {
		t.Fatal("ambiguous create response must make audit handoff retry")
	}
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatalf("idempotent retry did not verify committed record: %v", err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	if err := base.List(context.Background(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("durable records=%d err=%v", len(list.Items), err)
	}
}

func TestCommittedButUnobservedClaimNeverSendsBeforeRecovery(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtMostOnce)
	channel.Spec.Type = "scripted"
	base := outboxClient(t, audit, channel)
	c := &committedStatusErrorClient{Client: base, shouldErr: func(obj client.Object) bool {
		delivery, ok := obj.(*v1alpha1.PowerNotificationDelivery)
		return ok && delivery.Status.Phase == v1alpha1.NotificationDeliveryInProgress
	}}
	d := NewDispatcher(c, c)
	sender := &scriptedSender{}
	d.RegisterSender(sender)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	base.List(context.Background(), &list)
	key := client.ObjectKeyFromObject(&list.Items[0])
	if _, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err == nil {
		t.Fatal("ambiguous claim acknowledgement was reported successful")
	}
	if len(sender.events) != 0 {
		t.Fatal("provider called without an observed durable claim")
	}
	var claimed v1alpha1.PowerNotificationDelivery
	base.Get(context.Background(), key, &claimed)
	started := metav1.NewTime(time.Now().Add(-deliveryClaimTimeout - time.Second))
	claimed.Status.StartedAt = &started
	if err := base.Status().Update(context.Background(), &claimed); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	base.Get(context.Background(), key, &claimed)
	if claimed.Status.Phase != v1alpha1.NotificationDeliveryAmbiguous || len(sender.events) != 0 {
		t.Fatalf("claim recovery=%+v sends=%d", claimed.Status, len(sender.events))
	}
}

func TestCommittedButUnobservedCompletionAndProjectionAreIdempotent(t *testing.T) {
	for _, faultPhase := range []string{"delivery", "channel"} {
		t.Run(faultPhase, func(t *testing.T) {
			audit, channel, event := outboxFixture(deliveryPolicyAtLeastOnce)
			channel.Spec.Type = "scripted"
			base := outboxClient(t, audit, channel)
			c := &committedStatusErrorClient{Client: base, shouldErr: func(obj client.Object) bool {
				switch current := obj.(type) {
				case *v1alpha1.PowerNotificationDelivery:
					return faultPhase == "delivery" && current.Status.Phase == v1alpha1.NotificationDeliverySucceeded
				case *v1alpha1.PowerNotificationChannel:
					return faultPhase == "channel" && current.Status.LastAttempt != nil && current.Status.LastAttempt.Phase == "Succeeded"
				}
				return false
			}}
			d := NewDispatcher(c, c)
			sender := &scriptedSender{responses: []DeliveryResponse{{StatusCode: 204}}}
			d.RegisterSender(sender)
			if err := d.EnqueueContext(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			var list v1alpha1.PowerNotificationDeliveryList
			base.List(context.Background(), &list)
			if _, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[0])}); err != nil {
				t.Fatal(err)
			}
			var gotChannel v1alpha1.PowerNotificationChannel
			base.Get(context.Background(), client.ObjectKeyFromObject(channel), &gotChannel)
			var gotDelivery v1alpha1.PowerNotificationDelivery
			base.Get(context.Background(), client.ObjectKeyFromObject(&list.Items[0]), &gotDelivery)
			if len(sender.events) != 1 || gotChannel.Status.TotalSent != 1 || !gotDelivery.Status.ChannelStatusRecorded {
				t.Fatalf("sends=%d channel=%+v delivery=%+v", len(sender.events), gotChannel.Status, gotDelivery.Status)
			}
		})
	}
}

func TestAuditMaterializerClosesCommittedAuditHandoffAndSkipsLegacyHistory(t *testing.T) {
	audit, channel, _ := outboxFixture(deliveryPolicyAtMostOnce)
	audit.Labels = map[string]string{AuditOutboxLabel: AuditOutboxEnabled}
	legacy := audit.DeepCopy()
	legacy.Name = "legacy-audit"
	legacy.UID = "legacy-uid"
	legacy.Labels = nil
	c := outboxClient(t, audit, legacy, channel)
	d := NewDispatcher(c, c)
	r := &auditMaterializer{dispatcher: d}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(audit)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(legacy)}); err != nil {
		t.Fatal(err)
	}
	var deliveries v1alpha1.PowerNotificationDeliveryList
	if err := c.List(context.Background(), &deliveries); err != nil || len(deliveries.Items) != 1 || deliveries.Items[0].Spec.AuditEvent.Name != audit.Name {
		t.Fatalf("deliveries=%+v err=%v", deliveries.Items, err)
	}
}

func TestDeliveryLabelsHashLongObjectNames(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtMostOnce)
	audit.Name = strings.Repeat("a", 200)
	channel.Name = strings.Repeat("c", 200)
	event.AuditEventRef = audit.Namespace + "/" + audit.Name
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var deliveries v1alpha1.PowerNotificationDeliveryList
	if err := c.List(context.Background(), &deliveries); err != nil || len(deliveries.Items) != 1 {
		t.Fatalf("deliveries=%d err=%v", len(deliveries.Items), err)
	}
	for key, value := range deliveries.Items[0].Labels {
		if len(value) > 63 {
			t.Fatalf("label %s length=%d", key, len(value))
		}
	}
	if deliveries.Items[0].Spec.AuditEvent.Name != audit.Name || deliveries.Items[0].Spec.Channel.Name != channel.Name {
		t.Fatal("hashed labels lost exact object references")
	}
}

func TestDurableDeliverySendsStableIdempotencyHeaderAndPersistsSuccess(t *testing.T) {
	headers := make(chan string, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	audit, channel, event := outboxFixture(deliveryPolicyAtLeastOnce)
	channel.Spec.URL = receiver.URL
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	if err := c.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	key := client.ObjectKeyFromObject(&list.Items[0])
	if _, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-headers:
		if got != list.Items[0].Spec.IdempotencyKey {
			t.Fatalf("header=%q want=%q", got, list.Items[0].Spec.IdempotencyKey)
		}
	case <-time.After(time.Second):
		t.Fatal("receiver not called")
	}
	var got v1alpha1.PowerNotificationDelivery
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase != v1alpha1.NotificationDeliverySucceeded || got.Status.AttemptCount != 1 || !got.Status.ChannelStatusRecorded {
		t.Fatalf("unexpected terminal status: %+v", got.Status)
	}
}

type scriptedSender struct {
	mu        sync.Mutex
	events    []Event
	responses []DeliveryResponse
	errs      []error
	block     <-chan struct{}
}

func (s *scriptedSender) Type() string { return "scripted" }
func (s *scriptedSender) Send(ctx context.Context, _ string, event Event) error {
	_, err := s.SendWithResult(ctx, "", event)
	return err
}
func (s *scriptedSender) SendWithResult(ctx context.Context, _ string, event Event) (DeliveryResponse, error) {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return DeliveryResponse{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := len(s.events)
	s.events = append(s.events, event)
	var response DeliveryResponse
	var err error
	if i < len(s.responses) {
		response = s.responses[i]
	}
	if i < len(s.errs) {
		err = s.errs[i]
	}
	return response, err
}

func TestAtLeastOnceRetryUsesSameKeyAfterAmbiguousProviderOutcome(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtLeastOnce)
	channel.Spec.Type = "scripted"
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	sender := &scriptedSender{responses: []DeliveryResponse{{Attempts: 1}, {StatusCode: 204, Attempts: 1}}, errs: []error{context.DeadlineExceeded, nil}}
	d.RegisterSender(sender)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	c.List(context.Background(), &list)
	key := client.ObjectKeyFromObject(&list.Items[0])
	result, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	if err != nil || result.RequeueAfter <= 0 {
		t.Fatalf("first reconcile result=%+v err=%v", result, err)
	}
	var pending v1alpha1.PowerNotificationDelivery
	c.Get(context.Background(), key, &pending)
	past := metav1.NewTime(time.Now().Add(-time.Second))
	pending.Status.NextAttemptAt = &past
	if err := c.Status().Update(context.Background(), &pending); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if len(sender.events) != 2 || sender.events[0].IdempotencyKey == "" || sender.events[0].IdempotencyKey != sender.events[1].IdempotencyKey {
		t.Fatalf("replay keys not stable: %+v", sender.events)
	}
	var done v1alpha1.PowerNotificationDelivery
	c.Get(context.Background(), key, &done)
	if done.Status.Phase != v1alpha1.NotificationDeliverySucceeded || done.Status.AttemptCount != 2 {
		t.Fatalf("retry did not converge: %+v", done.Status)
	}
}

func TestAtMostOnceRestartMarksStaleClaimAmbiguousWithoutReplay(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtMostOnce)
	channel.Spec.Type = "scripted"
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	sender := &scriptedSender{}
	d.RegisterSender(sender)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	c.List(context.Background(), &list)
	item := list.Items[0]
	started := metav1.NewTime(time.Now().Add(-deliveryClaimTimeout - time.Second))
	item.Status = v1alpha1.PowerNotificationDeliveryStatus{Phase: v1alpha1.NotificationDeliveryInProgress, AttemptCount: 1, ActiveAttemptID: "old-claim", StartedAt: &started}
	if err := c.Status().Update(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&item)}); err != nil {
		t.Fatal(err)
	}
	if len(sender.events) != 0 {
		t.Fatal("ambiguous at-most-once claim was replayed")
	}
	var got v1alpha1.PowerNotificationDelivery
	c.Get(context.Background(), client.ObjectKeyFromObject(&item), &got)
	if got.Status.Phase != v1alpha1.NotificationDeliveryAmbiguous {
		t.Fatalf("phase=%s", got.Status.Phase)
	}
	var updatedChannel v1alpha1.PowerNotificationChannel
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(channel), &updatedChannel); err != nil {
		t.Fatal(err)
	}
	if updatedChannel.Status.LastAttempt == nil || updatedChannel.Status.LastAttempt.Phase != "Ambiguous" {
		t.Fatalf("channel lost ambiguous outcome: %+v", updatedChannel.Status.LastAttempt)
	}
}

func TestLateProviderCompletionCannotWinAfterClaimExpiry(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtLeastOnce)
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	c.List(context.Background(), &list)
	delivery := &list.Items[0]
	attemptID, err := d.claimDelivery(context.Background(), delivery, time.Now().Add(-deliveryClaimTimeout-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.returnDeliveryToPending(context.Background(), delivery, attemptID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := d.completeDelivery(context.Background(), delivery, attemptID, v1alpha1.NotificationDeliverySucceeded, DeliveryResponse{StatusCode: 204}, nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	var got v1alpha1.PowerNotificationDelivery
	c.Get(context.Background(), client.ObjectKeyFromObject(delivery), &got)
	if got.Status.Phase != v1alpha1.NotificationDeliveryPending || got.Status.LastAttemptID != "" {
		t.Fatalf("late response escaped claim fence: %+v", got.Status)
	}
}

func TestPreflightFailuresDoNotConsumeProviderAttemptBudget(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtMostOnce)
	channel.Spec.Type = "missing-sender"
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	c.List(context.Background(), &list)
	key := client.ObjectKeyFromObject(&list.Items[0])
	for i := 0; i < 25; i++ {
		if _, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
		var pending v1alpha1.PowerNotificationDelivery
		c.Get(context.Background(), key, &pending)
		past := metav1.NewTime(time.Now().Add(-time.Second))
		pending.Status.NextAttemptAt = &past
		if err := c.Status().Update(context.Background(), &pending); err != nil {
			t.Fatal(err)
		}
	}
	var got v1alpha1.PowerNotificationDelivery
	c.Get(context.Background(), key, &got)
	if got.Status.Phase != v1alpha1.NotificationDeliveryPending || got.Status.AttemptCount != 0 || got.Status.PreflightFailureCount != 25 {
		t.Fatalf("preflight consumed provider budget: %+v", got.Status)
	}
}

func TestConcurrentReconcilesIssueOnlyOneProviderRequest(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtLeastOnce)
	channel.Spec.Type = "scripted"
	c := outboxClient(t, audit, channel)
	d := NewDispatcher(c, c)
	release := make(chan struct{})
	sender := &scriptedSender{block: release, responses: []DeliveryResponse{{StatusCode: 204, Attempts: 1}}}
	d.RegisterSender(sender)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	c.List(context.Background(), &list)
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[0])}
	done := make(chan error, 1)
	go func() { _, err := d.Reconcile(context.Background(), req); done <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		var got v1alpha1.PowerNotificationDelivery
		c.Get(context.Background(), req.NamespacedName, &got)
		if got.Status.Phase == v1alpha1.NotificationDeliveryInProgress {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first claim not persisted")
		}
		time.Sleep(time.Millisecond)
	}
	if result, err := d.Reconcile(context.Background(), req); err != nil || result.RequeueAfter <= 0 {
		t.Fatalf("second reconcile result=%+v err=%v", result, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(sender.events) != 1 {
		t.Fatalf("provider requests=%d want=1", len(sender.events))
	}
}

func TestThrottleSurvivesRestartThroughTerminalDeliveryRecords(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtLeastOnce)
	channel.Spec.Type = "scripted"
	channel.Spec.Throttle = "1h"
	audit2 := audit.DeepCopy()
	audit2.Name = "audit-2"
	audit2.UID = types.UID("audit-uid-2")
	audit2.ResourceVersion = ""
	audit2.Spec.Timestamp = metav1.NewTime(audit.Spec.Timestamp.Add(time.Minute))
	c := outboxClient(t, audit, audit2, channel)
	d := NewDispatcher(c, c)
	sender := &scriptedSender{}
	d.RegisterSender(sender)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	event.AuditEventRef = "aura-system/audit-2"
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	if err := c.List(context.Background(), &list); err != nil {
		t.Fatal(err)
	}
	var first, second *v1alpha1.PowerNotificationDelivery
	for i := range list.Items {
		item := &list.Items[i]
		if item.Spec.AuditEvent.Name == "audit-1" {
			first = item
		} else {
			second = item
		}
	}
	completed := metav1.Now()
	first.Status = v1alpha1.PowerNotificationDeliveryStatus{Phase: v1alpha1.NotificationDeliverySucceeded, AttemptCount: 1, LastAttemptID: "done", CompletedAt: &completed, ChannelStatusRecorded: true}
	if err := c.Status().Update(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	result, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(second)})
	if err != nil || result.RequeueAfter < 50*time.Minute {
		t.Fatalf("throttle result=%+v err=%v", result, err)
	}
	if len(sender.events) != 0 {
		t.Fatal("durable throttle allowed an early provider request")
	}
}

func TestThrottleSerializesConcurrentDeliveriesForSameKey(t *testing.T) {
	audit, channel, event := outboxFixture(deliveryPolicyAtLeastOnce)
	channel.Spec.Type = "scripted"
	channel.Spec.Throttle = "1h"
	audit2 := audit.DeepCopy()
	audit2.Name, audit2.UID, audit2.ResourceVersion = "audit-2", "audit-uid-2", ""
	c := outboxClient(t, audit, audit2, channel)
	d := NewDispatcher(c, c)
	release := make(chan struct{})
	sender := &scriptedSender{block: release, responses: []DeliveryResponse{{StatusCode: 204}}}
	d.RegisterSender(sender)
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	event.AuditEventRef = "aura-system/audit-2"
	if err := d.EnqueueContext(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var list v1alpha1.PowerNotificationDeliveryList
	c.List(context.Background(), &list)
	for i := range list.Items {
		list.Items[i].CreationTimestamp = metav1.NewTime(time.Unix(int64(i+1), 0))
		if err := c.Update(context.Background(), &list.Items[i]); err != nil {
			t.Fatal(err)
		}
	}
	var first, second v1alpha1.PowerNotificationDelivery
	c.List(context.Background(), &list)
	for i := range list.Items {
		if first.Name == "" || list.Items[i].CreationTimestamp.Before(&first.CreationTimestamp) {
			second = first
			first = list.Items[i]
		} else {
			second = list.Items[i]
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&first)})
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		var claimed v1alpha1.PowerNotificationDelivery
		c.Get(context.Background(), client.ObjectKeyFromObject(&first), &claimed)
		if claimed.Status.Phase == v1alpha1.NotificationDeliveryInProgress {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("head delivery was not claimed")
		}
		time.Sleep(time.Millisecond)
	}
	result, err := d.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&second)})
	if err != nil || result.RequeueAfter <= 0 {
		t.Fatalf("non-head result=%+v err=%v", result, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(sender.events) != 1 {
		t.Fatalf("provider requests=%d want=1", len(sender.events))
	}
}

func TestDurableOutboxHasNoInMemoryQueueCapacityLimit(t *testing.T) {
	_, channel, _ := outboxFixture(deliveryPolicyAtMostOnce)
	objects := []client.Object{channel}
	for i := 0; i < 501; i++ {
		objects = append(objects, &v1alpha1.PowerAuditEvent{ObjectMeta: metav1.ObjectMeta{Name: fmtAudit(i), Namespace: "aura-system", UID: types.UID("audit-" + fmtAudit(i))}})
	}
	c := outboxClient(t, objects...)
	d := NewDispatcher(c, c)
	for i := 0; i < 501; i++ {
		if err := d.EnqueueContext(context.Background(), Event{AuditEventRef: "aura-system/" + fmtAudit(i), Action: "execution.error", Timestamp: time.Now()}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	var list v1alpha1.PowerNotificationDeliveryList
	c.List(context.Background(), &list)
	if len(list.Items) != 501 {
		t.Fatalf("deliveries=%d want=501", len(list.Items))
	}
}

func fmtAudit(i int) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyz"
	if i == 0 {
		return "a0"
	}
	s := ""
	for i > 0 {
		s = string(chars[i%len(chars)]) + s
		i /= len(chars)
	}
	return "a" + s
}
