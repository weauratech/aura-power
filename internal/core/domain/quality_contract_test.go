package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestQualityScopeIntersectsEverySelector(t *testing.T) {
	target := Target{Ref: WorkloadRef{Namespace: "dev", Name: "api"}, Labels: map[string]string{"app": "api"}, NamespaceLabels: map[string]string{"env": "dev"}}
	scope := Scope{Namespaces: []string{"dev"}, WorkloadNames: []string{"api"}, WorkloadLabels: map[string]string{"app": "api"}, NamespaceLabels: map[string]string{"env": "dev"}}
	if !MatchesScope(target, scope) {
		t.Fatal("all selectors match")
	}
	for _, field := range []string{"namespace", "name", "workload-label", "namespace-label"} {
		t.Run(field, func(t *testing.T) {
			other := target
			switch field {
			case "namespace":
				other.Ref.Namespace = "prod"
			case "name":
				other.Ref.Name = "worker"
			case "workload-label":
				other.Labels = map[string]string{"app": "worker"}
			case "namespace-label":
				other.NamespaceLabels = map[string]string{"env": "prod"}
			}
			if MatchesScope(other, scope) {
				t.Fatal("one mismatching selector must exclude target")
			}
		})
	}
}

func TestQualityPriorityPermutationAndInputImmutability(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	rules := []EvaluatedRule{
		{Ref: RuleRef{Name: "low", Priority: 1}, EffectiveState: PowerStateOn},
		{Ref: RuleRef{Name: "off", Priority: 2}, EffectiveState: PowerStateOff, Specificity: ScopeWorkload},
		{Ref: RuleRef{Name: "older", Priority: 2, CreatedAt: now.Add(-time.Hour)}, EffectiveState: PowerStateOn, Specificity: ScopeWorkload},
		{Ref: RuleRef{Name: "winner", Priority: 2, CreatedAt: now}, EffectiveState: PowerStateOn, Specificity: ScopeWorkload},
	}
	var visit func([]EvaluatedRule, int)
	visit = func(input []EvaluatedRule, n int) {
		if n == len(input) {
			before := append([]EvaluatedRule(nil), input...)
			winner, suppressed := ResolvePriority(input)
			if winner.Ref.Name != "winner" || len(suppressed) != 3 {
				t.Fatalf("unexpected priority result: %+v / %+v", winner, suppressed)
			}
			if !reflect.DeepEqual(input, before) {
				t.Fatal("priority mutated input")
			}
			return
		}
		for i := n; i < len(input); i++ {
			input[n], input[i] = input[i], input[n]
			visit(input, n+1)
			input[n], input[i] = input[i], input[n]
		}
	}
	visit(rules, 0)
}

func TestQualityWindowBoundariesTimezoneAndWeekRollover(t *testing.T) {
	window := TimeWindow{Start: TimeOfDay{22, 0}, End: TimeOfDay{6, 0}, Days: []Weekday{Saturday}, Timezone: "America/Sao_Paulo"}
	for _, tc := range []struct {
		instant string
		want    bool
	}{
		{"2026-09-13T00:59:59Z", false}, {"2026-09-13T01:00:00Z", true},
		{"2026-09-13T08:59:59Z", true}, {"2026-09-13T09:00:00Z", false},
		{"2026-09-14T03:00:00Z", false},
	} {
		t.Run(tc.instant, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.instant)
			if err != nil {
				t.Fatal(err)
			}
			if got := IsInWindow(window, now); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	// Both occurrences of a repeated wall-clock hour obey the same window.
	dst := TimeWindow{Start: TimeOfDay{1, 0}, End: TimeOfDay{2, 0}, Timezone: "America/New_York"}
	for _, hour := range []int{5, 6} {
		if !IsInWindow(dst, time.Date(2026, 11, 1, hour, 30, 0, 0, time.UTC)) {
			t.Fatal("repeated DST hour should be inside")
		}
	}
}

func TestQualityRestoreAllowedDespiteOwnershipGuardrails(t *testing.T) {
	replicas := int32(3)
	target := Target{Ref: WorkloadRef{Namespace: "kube-system", Name: "api", Kind: WorkloadKindDeployment}, Snapshot: &Snapshot{ReplicaCount: &replicas}, Ownership: []OwnershipSignal{{Type: OwnershipHPA}}}
	decision := ComputeDecision(target, []PolicySpec{{Name: "restore", Schedule: Schedule{DesiredState: PowerStateOn}}}, nil, DefaultGuardrailConfig(), time.Unix(0, 0))
	if decision.IsBlocked() || !decision.Divergent || decision.SnapshotRequired {
		t.Fatalf("restore must be allowed: %+v", decision)
	}
}
