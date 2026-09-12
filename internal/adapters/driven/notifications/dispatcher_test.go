package notifications

import (
	"context"
	"errors"
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
	d := NewDispatcher(c)
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
	if got.Status.TotalErrors != 1 || got.Status.TotalSent != 0 || got.Status.LastError != "controlled failure" {
		t.Fatalf("failure status not persisted: %+v", got.Status)
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

func TestEnqueueDropsOldestWhenQueueIsFull(t *testing.T) {
	d, _, _ := testDispatcher(t)
	d.queue = make(chan Event, 2)
	d.Enqueue(Event{Target: TargetRef{Name: "oldest"}})
	d.Enqueue(Event{Target: TargetRef{Name: "middle"}})
	d.Enqueue(Event{Target: TargetRef{Name: "newest"}})

	first := <-d.queue
	second := <-d.queue
	if first.Target.Name != "middle" || second.Target.Name != "newest" {
		t.Fatalf("queue retained wrong events: first=%q second=%q", first.Target.Name, second.Target.Name)
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
	key := channel.Name + "/" + eventIdentity(Event{Action: "execution.error", Target: TargetRef{Namespace: "team-a", Name: "api", Kind: "Deployment", UID: "uid-1"}})
	if _, throttled := d.throttle[key]; throttled {
		t.Fatal("failed delivery was incorrectly throttled")
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
