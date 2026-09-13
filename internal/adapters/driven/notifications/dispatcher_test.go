package notifications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weauratech/aura-power/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type flakyStatusClient struct {
	client.Client
	calls    int
	failAt   int
	failFrom int
}

type secretCacheRejectingClient struct {
	client.Client
	secretGets int
}

func (c *secretCacheRejectingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		c.secretGets++
		return errors.New("cached cluster-wide Secret list is forbidden")
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

type countingSecretReader struct {
	client.Reader
	secretGets int
}

func (r *countingSecretReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.Secret); ok {
		r.secretGets++
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func (c *flakyStatusClient) Status() client.SubResourceWriter {
	return &flakyStatusWriter{delegate: c.Client.Status(), owner: c}
}

type flakyStatusWriter struct {
	delegate client.SubResourceWriter
	owner    *flakyStatusClient
}

func (w *flakyStatusWriter) Create(ctx context.Context, obj client.Object, sub client.Object, opts ...client.SubResourceCreateOption) error {
	return w.delegate.Create(ctx, obj, sub, opts...)
}
func (w *flakyStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.owner.calls++
	if w.owner.failFrom > 0 && w.owner.calls >= w.owner.failFrom {
		return errors.New("sustained status outage")
	}
	if w.owner.calls == w.owner.failAt {
		return errors.New("transient status outage")
	}
	return w.delegate.Update(ctx, obj, opts...)
}
func (w *flakyStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return w.delegate.Patch(ctx, obj, patch, opts...)
}

type testSender struct {
	mu     sync.Mutex
	events []Event
	err    error
}

func (s *testSender) Type() string { return "fixture" }
func (s *testSender) Send(_ context.Context, _ string, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return s.err
}

func testDispatcher(t *testing.T, objects ...client.Object) (*Dispatcher, client.Client, *testSender) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.PowerNotificationChannel{}).WithObjects(objects...).Build()
	d := NewDispatcher(c, c)
	sender := &testSender{}
	d.RegisterSender(sender)
	return d, c, sender
}

func TestDispatchFiltersUpdatesStatusAndThrottles(t *testing.T) {
	matching := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "matching", Namespace: "aura-system"},
		Spec: v1alpha1.PowerNotificationChannelSpec{
			Type: "fixture", URL: "https://example.test", Enabled: true,
			Events: []string{"workload.restored"}, NamespaceFilter: []string{"team-a"}, Throttle: "1h",
		},
	}
	filtered := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "filtered", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "fixture", URL: "https://example.test", Enabled: true, Events: []string{"workload.error"}},
	}
	d, c, sender := testDispatcher(t, matching, filtered)
	event := Event{Action: "workload.restored", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}}
	d.dispatch(context.Background(), event)
	d.dispatch(context.Background(), event)
	if len(sender.events) != 1 {
		t.Fatalf("deliveries=%d want=1 after throttle", len(sender.events))
	}
	var got v1alpha1.PowerNotificationChannel
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(matching), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.TotalSent != 1 || got.Status.TotalErrors != 0 || got.Status.LastNotification == nil {
		t.Fatalf("success status not persisted: %+v", got.Status)
	}
}

func TestDispatchRecordsSenderFailure(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "failing", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "fixture", URL: "https://example.test", Enabled: true},
	}
	d, c, sender := testDispatcher(t, channel)
	sender.err = errors.New("controlled failure")
	d.dispatch(context.Background(), Event{Action: "workload.error", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}})
	var got v1alpha1.PowerNotificationChannel
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(channel), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.TotalErrors != 1 || got.Status.TotalSent != 0 || got.Status.LastError != redactedDeliveryFailure {
		t.Fatalf("failure status not persisted: %+v", got.Status)
	}
	if got.Status.LastAttempt == nil || got.Status.LastAttempt.Phase != "Failed" || got.Status.LastAttempt.Response != redactedDeliveryFailure {
		t.Fatalf("failure attempt correlation not persisted: %+v", got.Status.LastAttempt)
	}
}

func TestDeliveryPersistsAuditCorrelationAndRetriesStatusWithoutResending(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "correlated", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "fixture", URL: "https://example.test", Enabled: true},
	}
	d, base, sender := testDispatcher(t, channel)
	flaky := &flakyStatusClient{Client: base, failAt: 2} // fail the first final-status write
	d.client = flaky
	event := Event{
		AuditEventRef: "aura-system/evt-123", Action: "workload.restored", Result: "success", RuleName: "nightly",
		Timestamp: time.Unix(1700000000, 0).UTC(), Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment", UID: "uid-1"},
	}
	if err := d.dispatch(context.Background(), event); err != nil {
		t.Fatalf("transient status failure was not retried: %v", err)
	}
	if len(sender.events) != 1 {
		t.Fatalf("status retry resent provider request: deliveries=%d", len(sender.events))
	}
	var got v1alpha1.PowerNotificationChannel
	if err := base.Get(context.Background(), client.ObjectKeyFromObject(channel), &got); err != nil {
		t.Fatal(err)
	}
	attempt := got.Status.LastAttempt
	if attempt == nil || attempt.ID == "" || attempt.Phase != "Succeeded" || attempt.AttemptCount != 1 || attempt.Response != "accepted" {
		t.Fatalf("incomplete persisted attempt: %+v", attempt)
	}
	if len(attempt.EventIDs) != 1 || len(attempt.AuditEventRefs) != 1 || attempt.AuditEventRefs[0] != event.AuditEventRef {
		t.Fatalf("event to audit correlation lost: %+v", attempt)
	}
	if attempt.CompletedAt == nil || got.Status.TotalSent != 1 || flaky.calls < 3 {
		t.Fatalf("final status was not durably retried: status=%+v calls=%d", got.Status, flaky.calls)
	}
	if len(got.Status.RecentAttempts) != 1 || got.Status.RecentAttempts[0].ID != attempt.ID || got.Status.RecentAttempts[0].Phase != "Succeeded" {
		t.Fatalf("bounded correlation history not finalized: %+v", got.Status.RecentAttempts)
	}
}

func TestAttemptHistoryIsBounded(t *testing.T) {
	status := &v1alpha1.PowerNotificationChannelStatus{}
	for i := 0; i < maxRecentNotificationAttempts+3; i++ {
		storeAttempt(status, &v1alpha1.NotificationAttemptStatus{ID: fmt.Sprintf("attempt-%02d", i), Phase: "Succeeded"})
	}
	if len(status.RecentAttempts) != maxRecentNotificationAttempts || status.RecentAttempts[0].ID != "attempt-03" || status.LastAttempt.ID != "attempt-22" {
		t.Fatalf("attempt history is not bounded to the newest entries: %+v", status.RecentAttempts)
	}
}

func TestIncompleteAttemptIsRecoveredAfterRestartWithoutRedelivery(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "restart-recovery", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "fixture", URL: "https://example.test", Enabled: true},
	}
	d, base, sender := testDispatcher(t, channel)
	d.client = &flakyStatusClient{Client: base, failFrom: 2}
	event := Event{AuditEventRef: "aura-system/evt-restart", Action: "workload.restored", Timestamp: time.Now().UTC(), Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}}
	if err := d.dispatch(context.Background(), event); err == nil {
		t.Fatal("expected final status persistence to remain unavailable")
	}
	if len(sender.events) != 1 {
		t.Fatalf("provider deliveries=%d want=1", len(sender.events))
	}
	var interrupted v1alpha1.PowerNotificationChannel
	if err := base.Get(context.Background(), client.ObjectKeyFromObject(channel), &interrupted); err != nil {
		t.Fatal(err)
	}
	if interrupted.Status.LastAttempt == nil || interrupted.Status.LastAttempt.Phase != "InProgress" {
		t.Fatalf("interrupted attempt was not durably visible: %+v", interrupted.Status.LastAttempt)
	}

	restarted := NewDispatcher(base, base)
	restartedSender := &testSender{}
	restarted.RegisterSender(restartedSender)
	restartCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go restarted.Run(restartCtx)
	var recovered v1alpha1.PowerNotificationChannel
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := base.Get(context.Background(), client.ObjectKeyFromObject(channel), &recovered); err != nil {
			t.Fatal(err)
		}
		if recovered.Status.LastAttempt != nil && recovered.Status.LastAttempt.Phase == "Failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart did not recover attempt: %+v", recovered.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(restartedSender.events) != 0 {
		t.Fatalf("ambiguous delivery was replayed %d time(s)", len(restartedSender.events))
	}
	if recovered.Status.LastAttempt == nil || recovered.Status.LastAttempt.Phase != "Failed" || recovered.Status.LastAttempt.Response != unknownDeliveryOutcome || recovered.Status.LastAttempt.CompletedAt == nil {
		t.Fatalf("restart did not finalize ambiguous attempt: %+v", recovered.Status.LastAttempt)
	}
	if recovered.Status.TotalSent != 0 || recovered.Status.TotalErrors != 1 || recovered.Status.LastError != unknownDeliveryOutcome {
		t.Fatalf("restart recovery counters are not truthful: %+v", recovered.Status)
	}
}

func TestDispatchBatchDeduplicatesExactEvents(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "batch", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "fixture", URL: "https://example.test", Enabled: true},
	}
	d, _, sender := testDispatcher(t, channel)
	event := Event{Action: "workload.restored", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}, Reason: "fixture"}
	d.dispatchBatch(context.Background(), []Event{event, event})
	if len(sender.events) != 1 || sender.events[0].Reason != "fixture" {
		t.Fatalf("exact duplicate was not collapsed: %+v", sender.events)
	}
}

func TestDispatchBatchSeparatesDifferentActionsResultsAndRules(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "batch", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "fixture", URL: "https://example.test", Enabled: true},
	}
	d, c, sender := testDispatcher(t, channel)
	d.dispatchBatch(context.Background(), []Event{
		{Action: "workload.powered_down", Result: "success", RuleName: "nightly", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}},
		{Action: "workload.powered_down", Result: "success", RuleName: "nightly", Target: TargetRef{Namespace: "team-b", Name: "api", Kind: "StatefulSet"}},
		{Action: "workload.powered_down", Result: "blocked", RuleName: "nightly", Target: TargetRef{Namespace: "team-a", Name: "worker", Kind: "Deployment"}},
		{Action: "workload.powered_down", Result: "success", RuleName: "weekend", Target: TargetRef{Namespace: "team-a", Name: "cron", Kind: "CronJob"}},
		{Action: "workload.restored", Result: "success", RuleName: "nightly", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}},
	})

	if len(sender.events) != 4 {
		t.Fatalf("deliveries=%d want=4 semantically homogeneous messages: %+v", len(sender.events), sender.events)
	}
	if !strings.Contains(sender.events[0].Reason, "2 workload(s)") {
		t.Fatalf("same action/result/rule was not batched: %+v", sender.events[0])
	}
	for _, event := range sender.events {
		if event.Action == "" || event.Result == "" || event.RuleName == "" {
			t.Fatalf("delivery lost action/result/rule identity: %+v", event)
		}
	}
	var got v1alpha1.PowerNotificationChannel
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(channel), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.TotalSent != 4 {
		t.Fatalf("totalSent=%d want=4 deliveries", got.Status.TotalSent)
	}
}

func TestEnqueueFailsWithoutDroppingWhenQueueIsFull(t *testing.T) {
	d, _, _ := testDispatcher(t)
	d.queue = make(chan Event, 2)
	if err := d.Enqueue(Event{Target: TargetRef{Name: "oldest"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.Enqueue(Event{Target: TargetRef{Name: "middle"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.Enqueue(Event{Target: TargetRef{Name: "newest"}}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("enqueue error=%v want ErrQueueFull", err)
	}

	first := <-d.queue
	second := <-d.queue
	if first.Target.Name != "oldest" || second.Target.Name != "middle" {
		t.Fatalf("queue retained wrong events: first=%q second=%q", first.Target.Name, second.Target.Name)
	}
}

func TestExcludeAttemptedEventsUsesDurableAuditReference(t *testing.T) {
	status := v1alpha1.PowerNotificationChannelStatus{RecentAttempts: []v1alpha1.NotificationAttemptStatus{{
		AuditEventRefs: []string{"aura-system/action-already-attempted"},
		Phase:          "Succeeded",
	}}}
	events := []Event{
		{AuditEventRef: "aura-system/action-already-attempted", Target: TargetRef{Name: "duplicate"}},
		{AuditEventRef: "aura-system/action-pending", Target: TargetRef{Name: "pending"}},
		{Target: TargetRef{Name: "legacy-without-audit-ref"}},
	}
	filtered := excludeAttemptedEvents(status, events)
	if len(filtered) != 2 || filtered[0].Target.Name != "pending" || filtered[1].Target.Name != "legacy-without-audit-ref" {
		t.Fatalf("filtered events=%+v", filtered)
	}
}

func TestRedactDeliveryErrorNeverDependsOnLiteralURLReplacement(t *testing.T) {
	secretValue := "https://hooks.example.test/path\nPRIVATE-KEY-MATERIAL"
	transportError := errors.New("parse https://hooks.example.test/path%0APRIVATE-KEY-MATERIAL: invalid control character")
	safe := redactDeliveryError(transportError, secretValue).Error()
	for _, forbidden := range []string{"hooks.example.test", "PRIVATE-KEY-MATERIAL", "%0A"} {
		if strings.Contains(safe, forbidden) {
			t.Fatalf("sanitized error leaked %q: %q", forbidden, safe)
		}
	}
	if !strings.Contains(safe, "<redacted>") {
		t.Fatalf("sanitized error lacks explicit redaction marker: %q", safe)
	}
	if _, err := validateWebhookURL(secretValue); err == nil || strings.Contains(err.Error(), "PRIVATE-KEY-MATERIAL") {
		t.Fatalf("invalid secret-backed endpoint was not rejected safely: %v", err)
	}
	if _, err := validateWebhookURL("ftp://user:password@example.test/hook"); err == nil || strings.Contains(err.Error(), "password") {
		t.Fatalf("userinfo or unsupported scheme was not rejected safely: %v", err)
	}
}

func TestDispatchResolvesSecretInChannelNamespaceAndRedactsFailure(t *testing.T) {
	const webhookURL = "https://hooks.example.test/private-token"
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "aura-system"},
		Spec: v1alpha1.PowerNotificationChannelSpec{
			Type: "fixture", Enabled: true, URLFrom: &v1alpha1.SecretKeyRef{Name: "webhook", Key: "url"},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "webhook", Namespace: "aura-system"},
		Data:       map[string][]byte{"url": []byte(webhookURL)},
	}
	d, c, sender := testDispatcher(t, channel, secret)
	sender.err = errors.New("POST " + webhookURL + ": controlled failure")
	d.dispatch(context.Background(), Event{Action: "execution.error", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment", UID: "uid-1"}})

	var got v1alpha1.PowerNotificationChannel
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(channel), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.TotalErrors != 1 || got.Status.TotalSent != 0 {
		t.Fatalf("unexpected failure counters: %+v", got.Status)
	}
	if strings.Contains(got.Status.LastError, webhookURL) || !strings.Contains(got.Status.LastError, "<redacted>") {
		t.Fatalf("secret webhook URL was not redacted: %q", got.Status.LastError)
	}
	if got.Status.LastAttempt == nil || strings.Contains(got.Status.LastAttempt.Response, webhookURL) || !strings.Contains(got.Status.LastAttempt.Response, "<redacted>") {
		t.Fatalf("attempt response exposed secret webhook URL: %+v", got.Status.LastAttempt)
	}
	key := channel.Name + "/" + eventIdentity(Event{Action: "execution.error", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment", UID: "uid-1"}})
	if _, throttled := d.throttle[key]; throttled {
		t.Fatal("failed delivery was incorrectly throttled")
	}
}

func TestSecretResolutionBypassesManagerCache(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "secret", Namespace: "aura-system"},
		Spec: v1alpha1.PowerNotificationChannelSpec{
			Type: "fixture", Enabled: true, URLFrom: &v1alpha1.SecretKeyRef{Name: "webhook", Key: "url"},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "webhook", Namespace: "aura-system"},
		Data:       map[string][]byte{"url": []byte("https://example.test/hook")},
	}
	d, base, sender := testDispatcher(t, channel, secret)
	cacheClient := &secretCacheRejectingClient{Client: base}
	directReader := &countingSecretReader{Reader: base}
	d.client = cacheClient
	d.secretReader = directReader

	if err := d.dispatch(context.Background(), Event{Action: "workload.restored", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment"}}); err != nil {
		t.Fatalf("direct Secret read failed: %v", err)
	}
	if len(sender.events) != 1 {
		t.Fatalf("deliveries=%d want=1", len(sender.events))
	}
	if cacheClient.secretGets != 0 || directReader.secretGets != 1 {
		t.Fatalf("Secret read path cache=%d direct=%d, want cache=0 direct=1", cacheClient.secretGets, directReader.secretGets)
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	d, _, _ := testDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("dispatcher did not stop during readiness wait")
	}
}

func TestThrottleEntriesAreRemovedAfterExpiry(t *testing.T) {
	d, _, _ := testDispatcher(t)
	now := time.Now()
	d.throttle["expired"] = now.Add(-time.Second)
	d.throttle["active"] = now.Add(time.Minute)

	d.pruneThrottle(now)
	if _, ok := d.throttle["expired"]; ok {
		t.Fatal("expired throttle entry was retained")
	}
	if _, ok := d.throttle["active"]; !ok {
		t.Fatal("active throttle entry was removed")
	}
}
