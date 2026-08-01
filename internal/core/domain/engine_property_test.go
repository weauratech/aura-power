package domain

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// PBT-01: Determinism — same inputs always produce same output.
func TestPropertyDeterminism(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		target := drawTarget(t)
		policies := drawPolicies(t)
		overrides := drawOverrides(t)
		now := drawTime(t)
		config := DefaultGuardrailConfig()

		d1 := ComputeDecision(target, policies, overrides, config, now)
		d2 := ComputeDecision(target, policies, overrides, config, now)

		if d1.DesiredState != d2.DesiredState {
			t.Fatalf("non-deterministic: got %s then %s", d1.DesiredState, d2.DesiredState)
		}
		if d1.IsBlocked() != d2.IsBlocked() {
			t.Fatal("non-deterministic blocked state")
		}
	})
}

// PBT-02: Safety-first — at equal max priority, "on" always wins.
func TestPropertySafetyFirst(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		priority := Priority(rapid.IntRange(1, 100).Draw(t, "priority"))

		onRule := EvaluatedRule{
			Ref:            RuleRef{Kind: RuleKindPolicy, Name: "on-rule", Priority: priority, CreatedAt: time.Now()},
			EffectiveState: PowerStateOn,
			Specificity:    ScopeNamespace,
		}
		offRule := EvaluatedRule{
			Ref:            RuleRef{Kind: RuleKindPolicy, Name: "off-rule", Priority: priority, CreatedAt: time.Now()},
			EffectiveState: PowerStateOff,
			Specificity:    ScopeNamespace,
		}

		winner, _ := ResolvePriority([]EvaluatedRule{offRule, onRule})
		if winner.EffectiveState != PowerStateOn {
			t.Fatal("safety-first violated: 'on' should win at equal priority")
		}
	})
}

// PBT-03: Priority monotonicity — higher priority can never be suppressed by lower.
func TestPropertyPriorityMonotonicity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		highPriority := Priority(rapid.IntRange(50, 1000).Draw(t, "high"))
		lowPriority := Priority(rapid.IntRange(0, 49).Draw(t, "low"))

		highState := drawPowerState(t)
		lowState := drawPowerState(t)
		highSpec := ScopeSpecificity(rapid.IntRange(0, 2).Draw(t, "hspec"))
		lowSpec := ScopeSpecificity(rapid.IntRange(0, 2).Draw(t, "lspec"))

		highRule := EvaluatedRule{
			Ref:            RuleRef{Kind: RuleKindPolicy, Name: "high", Priority: highPriority, CreatedAt: time.Now()},
			EffectiveState: highState,
			Specificity:    highSpec,
		}
		lowRule := EvaluatedRule{
			Ref:            RuleRef{Kind: RuleKindPolicy, Name: "low", Priority: lowPriority, CreatedAt: time.Now()},
			EffectiveState: lowState,
			Specificity:    lowSpec,
		}

		winner, _ := ResolvePriority([]EvaluatedRule{lowRule, highRule})
		if winner.Ref.Priority != highPriority {
			t.Fatalf("priority monotonicity violated: expected %d to win, got %d", highPriority, winner.Ref.Priority)
		}
	})
}

// PBT-04: Override expiration inertness — expired overrides have zero effect.
func TestPropertyExpiredOverrideInert(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		target := drawTarget(t)
		target.Annotations = map[string]string{} // Ensure not exempt
		policies := drawPolicies(t)
		if len(policies) == 0 {
			policies = append(policies, PolicySpec{
				Name:     "default",
				Scope:    Scope{},
				Schedule: Schedule{DesiredState: PowerStateOff},
				Priority: 1,
			})
		}
		now := drawTime(t)
		config := DefaultGuardrailConfig()

		// Create an expired override
		expiredOverride := OverrideSpec{
			Name:      "expired",
			Scope:     Scope{}, // matches all
			State:     PowerStateOn,
			Priority:  9999, // Would win if active
			ExpiresAt: now.Add(-time.Hour), // Expired
			CreatedAt: now.Add(-2 * time.Hour),
		}

		withOverride := ComputeDecision(target, policies, []OverrideSpec{expiredOverride}, config, now)
		withoutOverride := ComputeDecision(target, policies, nil, config, now)

		if withOverride.DesiredState != withoutOverride.DesiredState {
			t.Fatal("expired override should have zero effect on decision")
		}
	})
}

// --- Drawing helpers using rapid ---

func drawTarget(t *rapid.T) Target {
	ns := rapid.SampledFrom([]string{"dev", "staging", "prod", "test"}).Draw(t, "ns")
	name := rapid.SampledFrom([]string{"api", "worker", "web", "cache", "db"}).Draw(t, "name")
	kind := rapid.SampledFrom([]WorkloadKind{WorkloadKindDeployment, WorkloadKindStatefulSet, WorkloadKindCronJob}).Draw(t, "kind")

	return Target{
		Ref:           WorkloadRef{Namespace: ns, Name: name, Kind: kind},
		ObservedState: ObservedState{Replicas: int32(rapid.IntRange(0, 10).Draw(t, "replicas"))},
		Annotations:   map[string]string{},
		Labels:        map[string]string{},
	}
}

func drawPolicies(t *rapid.T) []PolicySpec {
	count := rapid.IntRange(0, 5).Draw(t, "pcount")
	policies := make([]PolicySpec, 0, count)
	for i := 0; i < count; i++ {
		policies = append(policies, drawPolicySpec(t))
	}
	return policies
}

func drawOverrides(t *rapid.T) []OverrideSpec {
	count := rapid.IntRange(0, 3).Draw(t, "ocount")
	overrides := make([]OverrideSpec, 0, count)
	for i := 0; i < count; i++ {
		overrides = append(overrides, drawOverrideSpec(t))
	}
	return overrides
}

func drawPolicySpec(t *rapid.T) PolicySpec {
	return PolicySpec{
		Name:      rapid.SampledFrom([]string{"policy-a", "policy-b", "policy-c", "policy-d"}).Draw(t, "pname"),
		Namespace: "aura-system",
		Scope:     drawScope(t),
		Schedule:  Schedule{DesiredState: drawPowerState(t)},
		Priority:  Priority(rapid.IntRange(0, 100).Draw(t, "ppriority")),
		CreatedAt: drawTime(t),
	}
}

func drawOverrideSpec(t *rapid.T) OverrideSpec {
	now := time.Now()
	hoursAhead := rapid.IntRange(1, 72).Draw(t, "ohours")
	hoursAgo := rapid.IntRange(1, 24).Draw(t, "oage")
	return OverrideSpec{
		Name:      rapid.SampledFrom([]string{"override-x", "override-y", "override-z"}).Draw(t, "oname"),
		Namespace: "aura-system",
		Scope:     drawScope(t),
		State:     drawPowerState(t),
		Priority:  Priority(rapid.IntRange(0, 200).Draw(t, "opriority")),
		ExpiresAt: now.Add(time.Duration(hoursAhead) * time.Hour),
		CreatedAt: now.Add(-time.Duration(hoursAgo) * time.Hour),
	}
}

func drawScope(t *rapid.T) Scope {
	nsList := rapid.SampledFrom([]string{"dev", "staging", "prod", "test"}).Draw(t, "sns")
	useNs := rapid.Bool().Draw(t, "usens")
	if useNs {
		return Scope{Namespaces: []string{nsList}}
	}
	return Scope{}
}

func drawPowerState(t *rapid.T) PowerState {
	return rapid.SampledFrom([]PowerState{PowerStateOn, PowerStateOff}).Draw(t, "state")
}

func drawTime(t *rapid.T) time.Time {
	hour := rapid.IntRange(0, 23).Draw(t, "hour")
	minute := rapid.IntRange(0, 59).Draw(t, "minute")
	day := rapid.IntRange(1, 28).Draw(t, "day")
	return time.Date(2026, 7, day, hour, minute, 0, 0, time.UTC)
}
