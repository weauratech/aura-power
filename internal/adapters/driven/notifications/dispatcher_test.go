package notifications

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/weauratech/aura-power/api/v1alpha1"
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
