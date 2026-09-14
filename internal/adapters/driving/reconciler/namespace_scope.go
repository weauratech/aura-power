package reconciler

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func namespaceIsManaged(controlNamespace, candidate string) bool {
	return controlNamespace == "" || candidate == controlNamespace
}

func namespacePredicate(controlNamespace string) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return namespaceIsManaged(controlNamespace, obj.GetNamespace())
	})
}

func namespaceListOptions(controlNamespace string) []client.ListOption {
	if controlNamespace == "" {
		return nil
	}
	return []client.ListOption{client.InNamespace(controlNamespace)}
}
