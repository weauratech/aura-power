package kubernetes

import "github.com/weauratech/aura-power/internal/core/domain"

import v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"

func fromTargetRef(ref v1alpha1.TargetReference) domain.WorkloadRef {
	return domain.WorkloadRef{
		Namespace: ref.Namespace,
		Name:      ref.Name,
		Kind:      domain.WorkloadKind(ref.Kind),
	}
}
