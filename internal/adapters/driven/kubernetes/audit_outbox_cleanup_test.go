package kubernetes

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/notifications"
)

func TestAuditCleanupRetainsOutboxUntilTerminalIdentityRetentionExpires(t *testing.T) {
	now := time.Now().UTC()
	old := metav1.NewTime(now.Add(-40 * 24 * time.Hour))
	objects := make([]client.Object, 0, 8)
	for _, name := range []string{"pending", "in-progress", "recent-terminal", "expired-terminal"} {
		uid := types.UID("uid-" + name)
		audit := &v1alpha1.PowerAuditEvent{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "aura-system", UID: uid, CreationTimestamp: old,
			Labels: map[string]string{notifications.AuditOutboxLabel: notifications.AuditOutboxEnabled},
		}}
		digest := sha256.Sum256([]byte(string(uid)))
		delivery := &v1alpha1.PowerNotificationDelivery{ObjectMeta: metav1.ObjectMeta{
			Name: "delivery-" + name, Namespace: "aura-system",
			Labels: map[string]string{"power.aura.sh/audit-event": fmt.Sprintf("uid-%x", digest[:16])},
		}}
		switch name {
		case "pending":
			delivery.Status.Phase = v1alpha1.NotificationDeliveryPending
		case "in-progress":
			delivery.Status.Phase = v1alpha1.NotificationDeliveryInProgress
		case "recent-terminal":
			completed := metav1.NewTime(now.Add(-10 * 24 * time.Hour))
			delivery.Status = v1alpha1.PowerNotificationDeliveryStatus{Phase: v1alpha1.NotificationDeliverySucceeded, CompletedAt: &completed, ChannelStatusRecorded: true}
		case "expired-terminal":
			completed := metav1.NewTime(now.Add(-40 * 24 * time.Hour))
			delivery.Status = v1alpha1.PowerNotificationDeliveryStatus{Phase: v1alpha1.NotificationDeliverySucceeded, CompletedAt: &completed, ChannelStatusRecorded: true}
		}
		objects = append(objects, audit, delivery)
	}
	scheme := qualityScheme(t)
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.PowerNotificationDelivery{}).WithObjects(objects...).Build()
	recorder := NewAuditRecorderWithReader(c, c, nil, "aura-system")
	deleted, err := recorder.CleanupExpiredWithDeliveryRetention(context.Background(), 7, 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d want=1", deleted)
	}
	for _, name := range []string{"pending", "in-progress", "recent-terminal"} {
		if err := c.Get(context.Background(), client.ObjectKey{Namespace: "aura-system", Name: name}, &v1alpha1.PowerAuditEvent{}); err != nil {
			t.Fatalf("audit %s was deleted early: %v", name, err)
		}
	}
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "aura-system", Name: "expired-terminal"}, &v1alpha1.PowerAuditEvent{}); err == nil {
		t.Fatal("expired terminal audit identity was not collected")
	}
}

type deliveryListErrorReader struct{ client.Reader }

func (r deliveryListErrorReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*v1alpha1.PowerNotificationDeliveryList); ok {
		return errors.New("temporary delivery list failure")
	}
	return r.Reader.List(ctx, list, opts...)
}

func TestAuditCleanupFailsClosedWhenDeliveryStateCannotBeRead(t *testing.T) {
	old := metav1.NewTime(time.Now().Add(-40 * 24 * time.Hour))
	audit := &v1alpha1.PowerAuditEvent{ObjectMeta: metav1.ObjectMeta{Name: "audit", Namespace: "aura-system", UID: "uid-audit", CreationTimestamp: old, Labels: map[string]string{notifications.AuditOutboxLabel: notifications.AuditOutboxEnabled}}}
	scheme := qualityScheme(t)
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(audit).Build()
	recorder := NewAuditRecorderWithReader(c, deliveryListErrorReader{Reader: c}, nil, "aura-system")
	if _, err := recorder.CleanupExpiredWithDeliveryRetention(context.Background(), 7, 30); err == nil {
		t.Fatal("cleanup ignored unavailable delivery state")
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(audit), &v1alpha1.PowerAuditEvent{}); err != nil {
		t.Fatalf("audit was deleted while delivery state was unavailable: %v", err)
	}
}
