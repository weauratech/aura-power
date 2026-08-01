package domain

import (
	"testing"
	"time"
)

func TestComputeDecision_NoRules_ReturnsUnmanaged(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	decision := ComputeDecision(target, nil, nil, DefaultGuardrailConfig(), time.Now())

	if decision.IsManaged() {
		t.Fatal("expected unmanaged decision when no rules exist")
	}
}

func TestComputeDecision_SinglePolicy_Off(t *testing.T) {
	now := time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC) // Wednesday 22:00 UTC
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.ObservedState = ObservedState{Replicas: 3}

	policy := PolicySpec{
		Name:      "dev-off-hours",
		Namespace: "aura-system",
		Scope:     Scope{Namespaces: []string{"dev"}},
		Schedule: Schedule{
			Windows: []TimeWindow{{
				Start:    TimeOfDay{8, 0},
				End:      TimeOfDay{18, 0},
				Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
				Timezone: "UTC",
			}},
			DesiredState: PowerStateOn,
		},
		Priority: 10,
	}

	decision := ComputeDecision(target, []PolicySpec{policy}, nil, DefaultGuardrailConfig(), now)

	if decision.DesiredState != PowerStateOff {
		t.Fatalf("expected off at 22:00 (outside 08-18 window), got %s", decision.DesiredState)
	}
	if decision.WinningRule == nil {
		t.Fatal("expected winning rule")
	}
	if decision.WinningRule.Name != "dev-off-hours" {
		t.Fatalf("expected winning rule dev-off-hours, got %s", decision.WinningRule.Name)
	}
}

func TestComputeDecision_SinglePolicy_On(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC) // Wednesday 10:00 UTC
	target := makeTarget("dev", "api", WorkloadKindDeployment)

	policy := PolicySpec{
		Name:      "dev-off-hours",
		Namespace: "aura-system",
		Scope:     Scope{Namespaces: []string{"dev"}},
		Schedule: Schedule{
			Windows: []TimeWindow{{
				Start:    TimeOfDay{8, 0},
				End:      TimeOfDay{18, 0},
				Days:     []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday},
				Timezone: "UTC",
			}},
			DesiredState: PowerStateOn,
		},
		Priority: 10,
	}

	decision := ComputeDecision(target, []PolicySpec{policy}, nil, DefaultGuardrailConfig(), now)

	if decision.DesiredState != PowerStateOn {
		t.Fatalf("expected on at 10:00 (inside 08-18 window), got %s", decision.DesiredState)
	}
}

func TestComputeDecision_HigherPriorityWins(t *testing.T) {
	now := time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC)
	target := makeTarget("dev", "api", WorkloadKindDeployment)

	lowPolicy := PolicySpec{
		Name:      "low-priority",
		Scope:     Scope{Namespaces: []string{"dev"}},
		Schedule:  Schedule{DesiredState: PowerStateOff}, // 24/7 off
		Priority:  5,
		CreatedAt: now.Add(-time.Hour),
	}

	highOverride := OverrideSpec{
		Name:      "keep-alive",
		Scope:     Scope{Namespaces: []string{"dev"}},
		State:     PowerStateOn,
		Priority:  100,
		ExpiresAt: now.Add(3 * time.Hour),
		CreatedAt: now,
	}

	decision := ComputeDecision(target, []PolicySpec{lowPolicy}, []OverrideSpec{highOverride}, DefaultGuardrailConfig(), now)

	if decision.DesiredState != PowerStateOn {
		t.Fatal("expected on: higher priority override should win")
	}
	if decision.WinningRule.Name != "keep-alive" {
		t.Fatalf("expected winner keep-alive, got %s", decision.WinningRule.Name)
	}
}

func TestComputeDecision_SafetyFirst_EqualPriority(t *testing.T) {
	now := time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC)
	target := makeTarget("dev", "api", WorkloadKindDeployment)

	offPolicy := PolicySpec{
		Name:      "shut-down",
		Scope:     Scope{Namespaces: []string{"dev"}},
		Schedule:  Schedule{DesiredState: PowerStateOff},
		Priority:  10,
		CreatedAt: now.Add(-2 * time.Hour),
	}

	onPolicy := PolicySpec{
		Name:      "keep-on",
		Scope:     Scope{Namespaces: []string{"dev"}},
		Schedule:  Schedule{DesiredState: PowerStateOn},
		Priority:  10,
		CreatedAt: now.Add(-time.Hour),
	}

	decision := ComputeDecision(target, []PolicySpec{offPolicy, onPolicy}, nil, DefaultGuardrailConfig(), now)

	if decision.DesiredState != PowerStateOn {
		t.Fatal("expected on: safety-first rule says 'on' wins at equal priority")
	}
}

func TestComputeDecision_ExpiredOverrideIsInert(t *testing.T) {
	now := time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC)
	target := makeTarget("dev", "api", WorkloadKindDeployment)

	policy := PolicySpec{
		Name:     "dev-off",
		Scope:    Scope{Namespaces: []string{"dev"}},
		Schedule: Schedule{DesiredState: PowerStateOff},
		Priority: 10,
	}

	expiredOverride := OverrideSpec{
		Name:      "old-override",
		Scope:     Scope{Namespaces: []string{"dev"}},
		State:     PowerStateOn,
		Priority:  100,
		ExpiresAt: now.Add(-time.Hour), // Expired 1 hour ago
	}

	decision := ComputeDecision(target, []PolicySpec{policy}, []OverrideSpec{expiredOverride}, DefaultGuardrailConfig(), now)

	if decision.DesiredState != PowerStateOff {
		t.Fatal("expected off: expired override should be inert")
	}
	if decision.WinningRule.Name != "dev-off" {
		t.Fatalf("expected winner dev-off, got %s", decision.WinningRule.Name)
	}
}

func TestComputeDecision_ExemptTargetIsUnmanaged(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Annotations = map[string]string{"aura.sh/power-exempt": "true"}

	policy := PolicySpec{
		Name:     "dev-off",
		Scope:    Scope{Namespaces: []string{"dev"}},
		Schedule: Schedule{DesiredState: PowerStateOff},
		Priority: 10,
	}

	decision := ComputeDecision(target, []PolicySpec{policy}, nil, DefaultGuardrailConfig(), time.Now())

	if decision.IsManaged() {
		t.Fatal("expected exempt target to be unmanaged")
	}
}

func TestComputeDecision_SystemNamespaceBlocked(t *testing.T) {
	target := makeTarget("kube-system", "coredns", WorkloadKindDeployment)
	target.ObservedState = ObservedState{Replicas: 2}

	policy := PolicySpec{
		Name:     "everything-off",
		Scope:    Scope{}, // Matches all
		Schedule: Schedule{DesiredState: PowerStateOff},
		Priority: 10,
	}

	decision := ComputeDecision(target, []PolicySpec{policy}, nil, DefaultGuardrailConfig(), time.Now())

	if !decision.IsBlocked() {
		t.Fatal("expected system namespace workload to be blocked")
	}
	if decision.BlockReasons[0].Type != BlockSystemNamespace {
		t.Fatalf("expected BlockSystemNamespace, got %s", decision.BlockReasons[0].Type)
	}
}

func TestComputeDecision_SpecificityTieBreaker(t *testing.T) {
	now := time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC)

	target := makeTarget("dev", "api", WorkloadKindDeployment)

	nsPolicy := PolicySpec{
		Name:      "ns-level",
		Scope:     Scope{Namespaces: []string{"dev"}},
		Schedule:  Schedule{DesiredState: PowerStateOn},
		Priority:  10,
		CreatedAt: now.Add(-time.Hour),
	}

	wlPolicy := PolicySpec{
		Name:      "workload-level",
		Scope:     Scope{Namespaces: []string{"dev"}, WorkloadNames: []string{"api"}},
		Schedule:  Schedule{DesiredState: PowerStateOn},
		Priority:  10,
		CreatedAt: now.Add(-2 * time.Hour),
	}

	decision := ComputeDecision(target, []PolicySpec{nsPolicy, wlPolicy}, nil, DefaultGuardrailConfig(), now)

	if decision.WinningRule.Name != "workload-level" {
		t.Fatalf("expected workload-level to win by specificity, got %s", decision.WinningRule.Name)
	}
}

// Helpers

func makeTarget(namespace, name string, kind WorkloadKind) Target {
	return Target{
		Ref:           WorkloadRef{Namespace: namespace, Name: name, Kind: kind},
		ObservedState: ObservedState{Replicas: 1},
		Annotations:   map[string]string{},
		Labels:        map[string]string{},
	}
}
