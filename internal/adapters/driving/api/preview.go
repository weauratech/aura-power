package api

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/selection"
	"github.com/weauratech/aura-power/internal/core/domain"
)

type previewResponse struct {
	AffectedOn    []domain.WorkloadRef   `json:"affectedOn"`
	AffectedOff   []domain.WorkloadRef   `json:"affectedOff"`
	Blocked       []domain.BlockedTarget `json:"blocked"`
	Unsupported   []domain.WorkloadRef   `json:"unsupported"`
	Conflicts     []domain.ConflictInfo  `json:"conflicts"`
	TotalAffected int                    `json:"totalAffected"`
}

func newPreviewResponse(result domain.PreviewResult) previewResponse {
	return previewResponse{
		AffectedOn: result.AffectedOn, AffectedOff: result.AffectedOff, Blocked: result.Blocked,
		Unsupported: result.Unsupported, Conflicts: result.Conflicts, TotalAffected: result.TotalAffected,
	}
}

func previewTarget(t *v1alpha1.PowerTarget) domain.Target {
	result := domain.Target{
		Ref:           domain.WorkloadRef{Cluster: t.Spec.TargetRef.Cluster, APIVersion: t.Spec.TargetRef.APIVersion, Namespace: t.Spec.TargetRef.Namespace, Name: t.Spec.TargetRef.Name, Kind: domain.WorkloadKind(t.Spec.TargetRef.Kind), UID: t.Spec.TargetRef.UID},
		ObservedState: domain.ObservedState{Replicas: t.Status.ObservedState.Replicas, Suspended: t.Status.ObservedState.Suspended},
		Annotations:   t.Annotations, Labels: t.Status.WorkloadLabels, NamespaceLabels: t.Status.NamespaceLabels,
	}
	if t.Status.Snapshot != nil && t.Status.Snapshot.Available {
		result.Snapshot = &domain.Snapshot{ReplicaCount: t.Status.Snapshot.ReplicaCount, Suspended: t.Status.Snapshot.Suspended}
	}
	for _, ownership := range t.Status.Ownership {
		result.Ownership = append(result.Ownership, domain.OwnershipSignal{Type: domain.OwnershipType(ownership.Type), OptedIn: ownership.OptedIn})
	}
	return result
}

func previewPolicy(p *v1alpha1.PowerPolicy) domain.PolicySpec {
	windows := make([]domain.TimeWindow, 0, len(p.Spec.Schedule.Windows))
	for _, window := range p.Spec.Schedule.Windows {
		windows = append(windows, previewWindow(window))
	}
	return domain.PolicySpec{Name: p.Name, Namespace: p.Namespace, Scope: previewScope(p.Spec.Scope),
		Schedule: domain.Schedule{Windows: windows, DesiredState: domain.PowerState(p.Spec.Schedule.DesiredState)},
		Priority: domain.Priority(p.Spec.Priority), Description: p.Spec.Description, CreatedAt: p.CreationTimestamp.Time}
}

func previewOverride(o *v1alpha1.PowerOverride) domain.OverrideSpec {
	return domain.OverrideSpec{Name: o.Name, Namespace: o.Namespace, Scope: previewScope(o.Spec.Scope), State: domain.PowerState(o.Spec.State),
		Priority: domain.Priority(o.Spec.Priority), ExpiresAt: o.Spec.ExpiresAt.Time, Reason: o.Spec.Reason, Reference: o.Spec.Reference, CreatedAt: o.CreationTimestamp.Time}
}

func previewScope(scope v1alpha1.PolicyScope) domain.Scope {
	refs := make([]domain.WorkloadRef, 0, len(scope.TargetRefs))
	for _, ref := range scope.TargetRefs {
		refs = append(refs, domain.WorkloadRef{Cluster: ref.Cluster, APIVersion: ref.APIVersion, Namespace: ref.Namespace, Name: ref.Name, Kind: domain.WorkloadKind(ref.Kind), UID: ref.UID})
	}
	return domain.Scope{TargetRefs: refs, Namespaces: scope.Namespaces, NamespaceGroups: scope.NamespaceGroups,
		NamespaceLabels: scope.NamespaceLabels, WorkloadNames: scope.WorkloadNames, WorkloadLabels: scope.WorkloadLabels}
}

func previewWindow(window v1alpha1.TimeWindowSpec) domain.TimeWindow {
	var hour, minute int
	_, _ = fmt.Sscanf(window.Start, "%d:%d", &hour, &minute)
	start := domain.TimeOfDay{Hour: hour, Minute: minute}
	_, _ = fmt.Sscanf(window.End, "%d:%d", &hour, &minute)
	end := domain.TimeOfDay{Hour: hour, Minute: minute}
	days := make([]domain.Weekday, 0, len(window.Days))
	for _, day := range window.Days {
		days = append(days, domain.Weekday(day))
	}
	return domain.TimeWindow{Start: start, End: end, Days: days, Timezone: window.Timezone}
}

// resolvePreviewScopes applies the same NamespaceGroup expansion used by the
// reconciler. Invalid group references fail the preview instead of silently
// widening the proposed or existing rule.
func resolvePreviewPolicyScope(ctx context.Context, c client.Client, namespace string, policy *v1alpha1.PowerPolicy) (domain.Scope, error) {
	return selection.ResolveScope(ctx, c, namespace, policy.Spec.Scope)
}

func resolvePreviewOverrideScope(ctx context.Context, c client.Client, namespace string, override *v1alpha1.PowerOverride) (domain.Scope, error) {
	return selection.ResolveScope(ctx, c, namespace, override.Spec.Scope)
}
