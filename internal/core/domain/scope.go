package domain

// MatchesScope evaluates whether a target matches a scope using AND intersection logic.
// Empty selectors match everything. All non-empty selectors must match.
func MatchesScope(target Target, scope Scope) bool {
	if !matchesNamespaceNames(target, scope) {
		return false
	}
	if !matchesNamespaceLabels(target, scope) {
		return false
	}
	if !matchesWorkloadNames(target, scope) {
		return false
	}
	if !matchesWorkloadLabels(target, scope) {
		return false
	}
	return true
}

func matchesNamespaceNames(target Target, scope Scope) bool {
	if len(scope.Namespaces) == 0 {
		return true
	}
	for _, ns := range scope.Namespaces {
		if ns == target.Ref.Namespace {
			return true
		}
	}
	return false
}

func matchesNamespaceLabels(target Target, scope Scope) bool {
	if len(scope.NamespaceLabels) == 0 {
		return true
	}
	return labelsMatch(target.NamespaceLabels, scope.NamespaceLabels)
}

func matchesWorkloadNames(target Target, scope Scope) bool {
	if len(scope.WorkloadNames) == 0 {
		return true
	}
	for _, name := range scope.WorkloadNames {
		if name == target.Ref.Name {
			return true
		}
	}
	return false
}

func matchesWorkloadLabels(target Target, scope Scope) bool {
	if len(scope.WorkloadLabels) == 0 {
		return true
	}
	return labelsMatch(target.Labels, scope.WorkloadLabels)
}

// labelsMatch returns true if the target's labels contain all selector labels (subset check).
func labelsMatch(targetLabels, selectorLabels map[string]string) bool {
	if targetLabels == nil && len(selectorLabels) > 0 {
		return false
	}
	for key, value := range selectorLabels {
		if targetLabels[key] != value {
			return false
		}
	}
	return true
}

// ComputeSpecificity determines how specific a scope is.
func ComputeSpecificity(scope Scope) ScopeSpecificity {
	if len(scope.WorkloadNames) > 0 || len(scope.WorkloadLabels) > 0 {
		return ScopeWorkload
	}
	if len(scope.Namespaces) > 0 || len(scope.NamespaceLabels) > 0 {
		return ScopeNamespace
	}
	return ScopeClusterWide
}
