//go:build acceptance

package domain

import (
	"testing"
	"time"
)

func TestAcceptanceCTRL13PreviewOverrideReportsChangedTarget(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	target := Target{Ref: WorkloadRef{Namespace: "dev", Name: "api", Kind: WorkloadKindDeployment}}
	policy := PolicySpec{Name: "off", Schedule: Schedule{DesiredState: PowerStateOff}}
	override := OverrideSpec{Name: "incident", State: PowerStateOn, Priority: 100, ExpiresAt: now.Add(time.Hour)}
	got := PreviewOverride(override, []Target{target}, []PolicySpec{policy}, nil, DefaultGuardrailConfig(), now)
	if len(got.AffectedOn) != 1 || got.TotalAffected != 1 {
		t.Fatalf("CTRL-13: override changes off to on: expected one affected target, got %+v", got)
	}
}

func TestAcceptanceCTRL14EmptyLabelValueRequiresKeyPresence(t *testing.T) {
	target := Target{Ref: WorkloadRef{Namespace: "prod", Name: "api"}, Labels: map[string]string{"other": "value"}}
	if MatchesScope(target, Scope{WorkloadLabels: map[string]string{"eligible": ""}}) {
		t.Fatal("CTRL-14: missing label eligible must not match eligible='' selector")
	}
}

func TestAcceptanceCTRL15OverrideExpiresAtExactDeadline(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	o := OverrideSpec{Name: "expired", State: PowerStateOff, ExpiresAt: now}
	d := ComputeDecision(Target{}, nil, []OverrideSpec{o}, DefaultGuardrailConfig(), now)
	if d.IsManaged() {
		t.Fatalf("CTRL-15: override must be inert at expiresAt, got %+v", d)
	}
}
