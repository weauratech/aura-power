// Package selection translates Kubernetes selection data into the domain model.
package selection

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
)

// ResolveScope expands namespace group references in the rule namespace.
// Explicit namespaces and namespaces from groups form a union. All other
// selectors retain their documented AND semantics. Missing groups return an
// error so callers cannot accidentally evaluate a group-only rule globally.
func ResolveScope(ctx context.Context, c client.Client, ruleNamespace string, scope v1alpha1.PolicyScope) (domain.Scope, error) {
	resolved := domain.Scope{
		Namespaces:      append([]string(nil), scope.Namespaces...),
		NamespaceLabels: cloneMap(scope.NamespaceLabels),
		WorkloadNames:   append([]string(nil), scope.WorkloadNames...),
		WorkloadLabels:  cloneMap(scope.WorkloadLabels),
	}

	seen := make(map[string]struct{}, len(resolved.Namespaces))
	for _, namespace := range resolved.Namespaces {
		seen[namespace] = struct{}{}
	}
	for _, name := range scope.NamespaceGroups {
		var group v1alpha1.PowerNamespaceGroup
		if err := c.Get(ctx, types.NamespacedName{Namespace: ruleNamespace, Name: name}, &group); err != nil {
			return domain.Scope{}, fmt.Errorf("resolve namespace group %q in namespace %q: %w", name, ruleNamespace, err)
		}
		for _, namespace := range group.Spec.Namespaces {
			if _, exists := seen[namespace]; exists {
				continue
			}
			seen[namespace] = struct{}{}
			resolved.Namespaces = append(resolved.Namespaces, namespace)
		}
	}
	return resolved, nil
}

// Target projects exactly the discovery metadata used by scope matching.
func Target(target *v1alpha1.PowerTarget) domain.Target {
	return domain.Target{
		Ref: domain.WorkloadRef{
			Namespace: target.Spec.TargetRef.Namespace,
			Name:      target.Spec.TargetRef.Name,
			Kind:      domain.WorkloadKind(target.Spec.TargetRef.Kind),
		},
		Labels:          cloneMap(target.Status.WorkloadLabels),
		NamespaceLabels: cloneMap(target.Status.NamespaceLabels),
	}
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
