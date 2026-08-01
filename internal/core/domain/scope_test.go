package domain

import "testing"

func TestMatchesScope_EmptyScope_MatchesAll(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	scope := Scope{}

	if !MatchesScope(target, scope) {
		t.Fatal("empty scope should match all targets")
	}
}

func TestMatchesScope_NamespaceMatch(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	scope := Scope{Namespaces: []string{"dev", "staging"}}

	if !MatchesScope(target, scope) {
		t.Fatal("target in 'dev' should match scope with 'dev' in namespaces")
	}
}

func TestMatchesScope_NamespaceMismatch(t *testing.T) {
	target := makeTarget("prod", "api", WorkloadKindDeployment)
	scope := Scope{Namespaces: []string{"dev", "staging"}}

	if MatchesScope(target, scope) {
		t.Fatal("target in 'prod' should NOT match scope with only 'dev,staging'")
	}
}

func TestMatchesScope_WorkloadNameMatch(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	scope := Scope{Namespaces: []string{"dev"}, WorkloadNames: []string{"api", "worker"}}

	if !MatchesScope(target, scope) {
		t.Fatal("target 'api' should match scope with 'api' in workload names")
	}
}

func TestMatchesScope_WorkloadNameMismatch(t *testing.T) {
	target := makeTarget("dev", "cache", WorkloadKindDeployment)
	scope := Scope{Namespaces: []string{"dev"}, WorkloadNames: []string{"api", "worker"}}

	if MatchesScope(target, scope) {
		t.Fatal("target 'cache' should NOT match scope with only 'api,worker'")
	}
}

func TestMatchesScope_LabelMatch(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Labels = map[string]string{"app": "api", "team": "backend"}
	scope := Scope{WorkloadLabels: map[string]string{"app": "api"}}

	if !MatchesScope(target, scope) {
		t.Fatal("target with label app=api should match scope requiring app=api")
	}
}

func TestMatchesScope_LabelMismatch(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Labels = map[string]string{"app": "api"}
	scope := Scope{WorkloadLabels: map[string]string{"app": "worker"}}

	if MatchesScope(target, scope) {
		t.Fatal("target with app=api should NOT match scope requiring app=worker")
	}
}

func TestMatchesScope_ANDLogic(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Labels = map[string]string{"app": "api"}

	// Namespace matches but workload name does NOT
	scope := Scope{Namespaces: []string{"dev"}, WorkloadNames: []string{"worker"}}

	if MatchesScope(target, scope) {
		t.Fatal("AND logic: target must match ALL non-empty selectors")
	}
}

func TestComputeSpecificity_ClusterWide(t *testing.T) {
	scope := Scope{}
	if ComputeSpecificity(scope) != ScopeClusterWide {
		t.Fatal("empty scope should be cluster-wide")
	}
}

func TestComputeSpecificity_Namespace(t *testing.T) {
	scope := Scope{Namespaces: []string{"dev"}}
	if ComputeSpecificity(scope) != ScopeNamespace {
		t.Fatal("scope with only namespaces should be namespace-level")
	}
}

func TestComputeSpecificity_Workload(t *testing.T) {
	scope := Scope{Namespaces: []string{"dev"}, WorkloadNames: []string{"api"}}
	if ComputeSpecificity(scope) != ScopeWorkload {
		t.Fatal("scope with workload names should be workload-level")
	}
}
