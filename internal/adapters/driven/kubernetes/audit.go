package kubernetes

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/ports"
)

type AuditRecorder struct {
	client    client.Client
	recorder  record.EventRecorder
	namespace string
}

func NewAuditRecorder(c client.Client, recorder record.EventRecorder, namespace string) *AuditRecorder {
	return &AuditRecorder{client: c, recorder: recorder, namespace: namespace}
}

func (a *AuditRecorder) Record(ctx context.Context, event ports.AuditEvent) error {
	// Create PowerAuditEvent CRD
	auditEvent := &v1alpha1.PowerAuditEvent{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "evt-",
			Namespace:    a.namespace,
			Labels: map[string]string{
				"power.aura.sh/action":           string(event.Action),
				"power.aura.sh/target-namespace": event.Target.Namespace,
				"power.aura.sh/target-name":      event.Target.Name,
			},
		},
		Spec: v1alpha1.PowerAuditEventSpec{
			Timestamp: metav1.NewTime(event.Timestamp),
			Action:    string(event.Action),
			Actor:     event.Actor,
			Target: v1alpha1.TargetReference{
				Namespace: event.Target.Namespace,
				Name:      event.Target.Name,
				Kind:      string(event.Target.Kind),
			},
			Result:   event.Result,
			Reason:   event.Reason,
			RuleName: event.RuleName,
		},
	}

	if err := a.client.Create(ctx, auditEvent); err != nil {
		return fmt.Errorf("failed to create audit event: %w", err)
	}

	return nil
}

func (a *AuditRecorder) List(ctx context.Context, opts ports.AuditListOptions) ([]ports.AuditEvent, error) {
	var list v1alpha1.PowerAuditEventList
	listOpts := []client.ListOption{client.InNamespace(a.namespace)}

	if opts.Target != nil {
		listOpts = append(listOpts, client.MatchingLabels{
			"power.aura.sh/target-namespace": opts.Target.Namespace,
			"power.aura.sh/target-name":      opts.Target.Name,
		})
	}
	if opts.Action != nil {
		listOpts = append(listOpts, client.MatchingLabels{
			"power.aura.sh/action": string(*opts.Action),
		})
	}

	if err := a.client.List(ctx, &list, listOpts...); err != nil {
		return nil, err
	}

	var events []ports.AuditEvent
	for _, item := range list.Items {
		if opts.Since != nil && item.Spec.Timestamp.Time.Before(*opts.Since) {
			continue
		}
		events = append(events, ports.AuditEvent{
			Timestamp: item.Spec.Timestamp.Time,
			Action:    ports.AuditAction(item.Spec.Action),
			Actor:     item.Spec.Actor,
			Target:    fromTargetRef(item.Spec.Target),
			Result:    item.Spec.Result,
			Reason:    item.Spec.Reason,
			RuleName:  item.Spec.RuleName,
		})
		if opts.Limit > 0 && len(events) >= opts.Limit {
			break
		}
	}

	return events, nil
}

// EmitKubernetesEvent emits a standard K8s Event for visibility in kubectl.
func (a *AuditRecorder) EmitKubernetesEvent(obj client.Object, eventType, reason, message string) {
	if eventType == "" {
		eventType = corev1.EventTypeNormal
	}
	a.recorder.Event(obj, eventType, reason, message)
}

// CleanupExpired deletes PowerAuditEvents older than the retention period.
func (a *AuditRecorder) CleanupExpired(ctx context.Context, retentionDays int) (int, error) {
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	var list v1alpha1.PowerAuditEventList
	if err := a.client.List(ctx, &list, client.InNamespace(a.namespace)); err != nil {
		return 0, err
	}

	deleted := 0
	for _, item := range list.Items {
		if item.CreationTimestamp.Time.Before(cutoff) {
			if err := a.client.Delete(ctx, &item); err == nil {
				deleted++
			}
		}
	}

	return deleted, nil
}
