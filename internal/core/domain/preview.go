package domain

import "time"

// PreviewPolicy simulates the impact of a new/modified policy without persistence.
// Read-only: no state mutation. Calling multiple times returns the same result.
func PreviewPolicy(
	policy PolicySpec,
	targets []Target,
	existingPolicies []PolicySpec,
	existingOverrides []OverrideSpec,
	config GuardrailConfig,
	now time.Time,
) PreviewResult {
	// Build hypothetical policy set
	hypotheticalPolicies := make([]PolicySpec, 0, len(existingPolicies)+1)
	hypotheticalPolicies = append(hypotheticalPolicies, existingPolicies...)
	hypotheticalPolicies = append(hypotheticalPolicies, policy)

	return computePreview(targets, existingPolicies, hypotheticalPolicies, existingOverrides, config, now)
}

// PreviewOverride simulates the impact of a new override without persistence.
func PreviewOverride(
	override OverrideSpec,
	targets []Target,
	existingPolicies []PolicySpec,
	existingOverrides []OverrideSpec,
	config GuardrailConfig,
	now time.Time,
) PreviewResult {
	// Build hypothetical override set
	hypotheticalOverrides := make([]OverrideSpec, 0, len(existingOverrides)+1)
	hypotheticalOverrides = append(hypotheticalOverrides, existingOverrides...)
	hypotheticalOverrides = append(hypotheticalOverrides, override)

	return computePreview(targets, existingPolicies, existingPolicies, hypotheticalOverrides, config, now)
}

func computePreview(
	targets []Target,
	currentPolicies []PolicySpec,
	newPolicies []PolicySpec,
	overrides []OverrideSpec,
	config GuardrailConfig,
	now time.Time,
) PreviewResult {
	result := PreviewResult{
		AffectedOn:  make([]WorkloadRef, 0),
		AffectedOff: make([]WorkloadRef, 0),
		Blocked:     make([]BlockedTarget, 0),
		Unsupported: make([]WorkloadRef, 0),
		Conflicts:   make([]ConflictInfo, 0),
	}

	for _, target := range targets {
		if IsExempt(target, config) {
			continue
		}

		// Compute decision with NEW rules
		newDecision := ComputeDecision(target, newPolicies, overrides, config, now)

		if !newDecision.IsManaged() {
			continue
		}

		// Compute decision with CURRENT rules (for comparison)
		currentDecision := ComputeDecision(target, currentPolicies, overrides, config, now)

		// Determine impact
		if newDecision.IsBlocked() {
			result.Blocked = append(result.Blocked, BlockedTarget{
				Ref:     target.Ref,
				Reasons: newDecision.BlockReasons,
			})
		} else if currentDecision.DesiredState != newDecision.DesiredState || !currentDecision.IsManaged() {
			// State changed or newly managed
			switch newDecision.DesiredState {
			case PowerStateOn:
				result.AffectedOn = append(result.AffectedOn, target.Ref)
			case PowerStateOff:
				result.AffectedOff = append(result.AffectedOff, target.Ref)
			}
		}

		// Record conflicts (multiple rules competing)
		if len(newDecision.SuppressedRules) > 0 && newDecision.WinningRule != nil {
			result.Conflicts = append(result.Conflicts, ConflictInfo{
				Target:     target.Ref,
				Winner:     *newDecision.WinningRule,
				Suppressed: newDecision.SuppressedRules,
			})
		}
	}

	result.TotalAffected = len(result.AffectedOn) + len(result.AffectedOff) + len(result.Blocked)
	return result
}
