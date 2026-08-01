package domain

import (
	"testing"

	"pgregory.net/rapid"
)

// PBT-06: Scope AND semantics
func TestPropertyScopeANDSemantics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ns := rapid.SampledFrom([]string{"dev", "staging", "prod"}).Draw(t, "ns")
		name := rapid.SampledFrom([]string{"api", "worker", "web"}).Draw(t, "name")

		target := Target{
			Ref:    WorkloadRef{Namespace: ns, Name: name, Kind: WorkloadKindDeployment},
			Labels: map[string]string{"app": name},
		}

		// Scope with both namespace AND workload name = AND logic
		scope := Scope{
			Namespaces:    []string{ns},
			WorkloadNames: []string{"different-name"},
		}

		// Target matches namespace but NOT workload name → should NOT match
		if MatchesScope(target, scope) {
			t.Fatal("AND semantics violated: target matched despite workload name mismatch")
		}
	})
}

// PBT: Empty scope matches everything
func TestPropertyEmptyScopeMatchesAll(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ns := rapid.SampledFrom([]string{"dev", "staging", "prod", "test"}).Draw(t, "ns")
		name := rapid.SampledFrom([]string{"api", "worker", "web", "cache"}).Draw(t, "name")

		target := Target{
			Ref:    WorkloadRef{Namespace: ns, Name: name, Kind: WorkloadKindDeployment},
			Labels: map[string]string{},
		}

		if !MatchesScope(target, Scope{}) {
			t.Fatal("empty scope should match all targets")
		}
	})
}
