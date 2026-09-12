package kubernetes

import "github.com/weauratech/aura-power/internal/core/domain"

import v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"

func fromTargetRef(ref v1alpha1.TargetReference) domain.WorkloadRef {
	return domain.WorkloadRef{
		Cluster:    ref.Cluster,
		APIVersion: ref.APIVersion,
		Namespace:  ref.Namespace,
		Name:       ref.Name,
		Kind:       domain.WorkloadKind(ref.Kind),
		UID:        ref.UID,
	}
}
