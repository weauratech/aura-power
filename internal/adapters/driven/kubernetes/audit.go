package kubernetes

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/notifications"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

type AuditRecorder struct {
	client    client.Client
	reader    client.Reader
	recorder  record.EventRecorder
	namespace string
	notifier  notificationEnqueuer
}

type notificationEnqueuer interface {
	Enqueue(notifications.Event) error
}

func NewAuditRecorder(c client.Client, recorder record.EventRecorder, namespace string) *AuditRecorder {
	return NewAuditRecorderWithReader(c, c, recorder, namespace)
}

// NewAuditRecorderWithReader keeps writes on the manager client while allowing
// history reads to bypass its informer cache.
func NewAuditRecorderWithReader(c client.Client, reader client.Reader, recorder record.EventRecorder, namespace string) *AuditRecorder {
	return &AuditRecorder{client: c, reader: reader, recorder: recorder, namespace: namespace}
}

// SetNotifier attaches a notification dispatcher to the audit recorder.
func (a *AuditRecorder) SetNotifier(n notificationEnqueuer) {
	a.notifier = n
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
				"power.aura.sh/target-kind":      string(event.Target.Kind),
				"power.aura.sh/target-uid":       event.Target.UID,
			},
		},
		Spec: v1alpha1.PowerAuditEventSpec{
			Timestamp: metav1.NewTime(event.Timestamp),
			Action:    string(event.Action),
			Actor:     event.Actor,
			Target: v1alpha1.AuditResourceReference{
				Cluster:    event.Target.Cluster,
				APIVersion: event.Target.APIVersion,
				Namespace:  event.Target.Namespace,
				Name:       event.Target.Name,
				Kind:       string(event.Target.Kind),
				UID:        event.Target.UID,
			},
			Result:                              event.Result,
			Reason:                              event.Reason,
			RuleName:                            event.RuleName,
			NotificationSuppressed:              event.SuppressNotification,
			NotificationSuppressionSource:       event.NotificationSuppressionSource,
			NotificationSuppressionNamespaceUID: event.NotificationSuppressionNamespaceUID,
		},
	}
	if event.ID != "" {
		auditEvent.Name = event.ID
		auditEvent.GenerateName = ""
	}
	if event.SuppressNotification {
		auditEvent.Labels["power.aura.sh/notification-suppressed"] = "true"
	}

	if err := a.client.Create(ctx, auditEvent); err != nil {
		if event.ID != "" && apierrors.IsAlreadyExists(err) {
			var existing v1alpha1.PowerAuditEvent
			if getErr := a.reader.Get(ctx, client.ObjectKey{Namespace: a.namespace, Name: event.ID}, &existing); getErr != nil {
				return fmt.Errorf("failed to verify existing audit event: %w", getErr)
			}
			if !equality.Semantic.DeepEqual(existing.Spec, auditEvent.Spec) {
				return fmt.Errorf("audit event %s/%s already exists with different semantics", a.namespace, event.ID)
			}
			auditEvent = &existing
		} else {
			return fmt.Errorf("failed to create audit event: %w", err)
		}
	}

	// Dispatch notification only for real state transitions (not routine reconciliation).
	if a.notifier != nil && !auditEvent.Spec.NotificationSuppressed && isNotifiableAction(string(event.Action)) {
		if err := a.notifier.Enqueue(notifications.Event{
			AuditEventRef: fmt.Sprintf("%s/%s", auditEvent.Namespace, auditEvent.Name),
			Action:        string(event.Action),
			Target:        notifications.TargetRef{Namespace: event.Target.Namespace, Name: event.Target.Name, Kind: string(event.Target.Kind), UID: event.Target.UID},
			Result:        event.Result,
			Reason:        event.Reason,
			RuleName:      event.RuleName,
			Timestamp:     event.Timestamp,
		}); err != nil {
			return fmt.Errorf("audit event persisted but notification enqueue must be retried: %w", err)
		}
	}

	return nil
}

// isNotifiableAction returns true for events that should trigger webhook notifications.
func isNotifiableAction(action string) bool {
	switch action {
	case "workload.powered_down", "workload.restored", "execution.error",
		"override.created", "override.expired", "policy.created", "policy.deleted":
		return true
	default:
		return false
	}
}

func (a *AuditRecorder) List(ctx context.Context, opts ports.AuditListOptions) ([]ports.AuditEvent, error) {
	listOpts := []client.ListOption{client.InNamespace(a.namespace)}

	if opts.Target != nil {
		labels := map[string]string{
			"power.aura.sh/target-namespace": opts.Target.Namespace,
			"power.aura.sh/target-name":      opts.Target.Name,
		}
		if opts.Target.Kind != "" {
			labels["power.aura.sh/target-kind"] = string(opts.Target.Kind)
		}
		if opts.Target.UID != "" {
			labels["power.aura.sh/target-uid"] = opts.Target.UID
		}
		listOpts = append(listOpts, client.MatchingLabels(labels))
	}
	if opts.Action != nil {
		listOpts = append(listOpts, client.MatchingLabels{
			"power.aura.sh/action": string(*opts.Action),
		})
	}

	limit := opts.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	events := make([]ports.AuditEvent, 0, min(limit, 200))
	continueToken := ""
	for len(events) < limit {
		var list v1alpha1.PowerAuditEventList
		pageOpts := append([]client.ListOption{}, listOpts...)
		pageOpts = append(pageOpts, &client.ListOptions{Raw: &metav1.ListOptions{Limit: 200, Continue: continueToken}})
		if err := a.reader.List(ctx, &list, pageOpts...); err != nil {
			return nil, err
		}
		for i := range list.Items {
			item := &list.Items[i]
			if opts.Since != nil && item.Spec.Timestamp.Time.Before(*opts.Since) {
				continue
			}
			events = append(events, toDomainAuditEvent(item))
			if len(events) >= limit {
				break
			}
		}
		continueToken = list.Continue
		if continueToken == "" {
			break
		}
	}

	return events, nil
}

func toDomainAuditEvent(item *v1alpha1.PowerAuditEvent) ports.AuditEvent {
	return ports.AuditEvent{
		Timestamp: item.Spec.Timestamp.Time,
		Action:    ports.AuditAction(item.Spec.Action),
		Actor:     item.Spec.Actor,
		Target: domain.WorkloadRef{
			Cluster: item.Spec.Target.Cluster, APIVersion: item.Spec.Target.APIVersion,
			Namespace: item.Spec.Target.Namespace, Name: item.Spec.Target.Name,
			Kind: domain.WorkloadKind(item.Spec.Target.Kind), UID: item.Spec.Target.UID,
		},
		Result: item.Spec.Result, Reason: item.Spec.Reason, RuleName: item.Spec.RuleName,
		SuppressNotification:                item.Spec.NotificationSuppressed,
		NotificationSuppressionSource:       item.Spec.NotificationSuppressionSource,
		NotificationSuppressionNamespaceUID: item.Spec.NotificationSuppressionNamespaceUID,
	}
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

	const pageSize int64 = 200
	const maxDeletesPerRun = 500
	deleted := 0
	continueToken := ""
	for {
		var list v1alpha1.PowerAuditEventList
		if err := a.reader.List(ctx, &list, &client.ListOptions{
			Namespace: a.namespace,
			Raw:       &metav1.ListOptions{Limit: pageSize, Continue: continueToken},
		}); err != nil {
			return deleted, err
		}
		for i := range list.Items {
			item := &list.Items[i]
			if item.CreationTimestamp.Time.Before(cutoff) {
				if err := a.client.Delete(ctx, item); err == nil {
					deleted++
					if deleted >= maxDeletesPerRun {
						return deleted, nil
					}
				}
			}
		}
		continueToken = list.Continue
		if continueToken == "" {
			break
		}
	}

	return deleted, nil
}
