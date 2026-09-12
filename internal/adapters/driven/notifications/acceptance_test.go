//go:build acceptance

package notifications

import (
	"context"
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

type recordingSender struct {
	mu     sync.Mutex
	events []Event
}

func (s *recordingSender) Type() string { return "generic" }
func (s *recordingSender) Send(_ context.Context, _ string, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func acceptanceDispatcher(t *testing.T, objects ...client.Object) (*Dispatcher, *recordingSender) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.PowerNotificationChannel{}).WithObjects(objects...).Build()
	dispatcher := NewDispatcher(c)
	sender := &recordingSender{}
	dispatcher.RegisterSender(sender)
	return dispatcher, sender
}

func TestAcceptanceCancelledDeliveryStopsWithoutBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := httpPost(ctx, "http://127.0.0.1:1", map[string]string{"event": "fixture"}); err == nil {
		t.Fatal("cancelled delivery reported success")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("cancelled delivery retried for %s", elapsed)
	}
}

func TestAcceptanceNotificationResolvesSecretURL(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "secret-backed", Namespace: "aura-system"},
		Spec: v1alpha1.PowerNotificationChannelSpec{
			Type:    "generic",
			Enabled: true,
			URLFrom: &v1alpha1.SecretKeyRef{Name: "webhook", Key: "url"},
		},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "webhook", Namespace: "aura-system"}, Data: map[string][]byte{"url": []byte("https://example.test/hook")}}
	dispatcher, sender := acceptanceDispatcher(t, channel, secret)
	dispatcher.dispatchBatch(context.Background(), []Event{{Action: "workload.restored", Target: TargetRef{Namespace: "fixture", Name: "api", Kind: "Deployment"}}})
	if len(sender.events) != 1 {
		t.Fatalf("secret-backed enabled channel delivered %d notifications, want 1", len(sender.events))
	}
}

func TestAcceptanceNotificationKeepsHomonymousKinds(t *testing.T) {
	channel := &v1alpha1.PowerNotificationChannel{
		ObjectMeta: metav1.ObjectMeta{Name: "inline", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerNotificationChannelSpec{Type: "generic", Enabled: true, URL: "https://example.test/hook"},
	}
	dispatcher, sender := acceptanceDispatcher(t, channel)
	dispatcher.dispatchBatch(context.Background(), []Event{
		{Action: "workload.powered_down", Target: TargetRef{Namespace: "fixture", Name: "shared", Kind: "Deployment"}},
		{Action: "workload.powered_down", Target: TargetRef{Namespace: "fixture", Name: "shared", Kind: "StatefulSet"}},
	})
	if len(sender.events) != 1 {
		t.Fatalf("batch deliveries=%d want=1", len(sender.events))
	}
	if !strings.Contains(sender.events[0].Reason, "2") {
		t.Fatalf("batch did not preserve both workload identities: reason=%q", sender.events[0].Reason)
	}
}
