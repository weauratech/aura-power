package domain

import (
	"slices"
	"time"
)

// ComputeDecision computes the effective desired state for a single target.
// Pure function: no side effects, deterministic, property-testable.
func ComputeDecision(target Target, policies []PolicySpec, overrides []OverrideSpec, config GuardrailConfig, now time.Time) Decision {
	// Exempt targets are excluded from all governance
	if IsExempt(target, config) {
		return Decision{}
	}

	// Step 1: Collect and evaluate applicable rules
	rules := collectApplicableRules(target, policies, overrides, now)

	if len(rules) == 0 {
		// No applicable rules — target is unmanaged
		return Decision{}
	}

	// Step 2: Resolve priority and determine winner
	winner, suppressed := ResolvePriority(rules)

	// Step 3: Build decision
	decision := Decision{
		DesiredState: winner.EffectiveState,
		WinningRule:  &winner.Ref,
		SuppressedRules: func() []RuleRef {
			refs := make([]RuleRef, 0, len(suppressed))
			for _, s := range suppressed {
				refs = append(refs, s.Ref)
			}
			return refs
		}(),
	}

	// Step 4: Evaluate guardrails (only if desired state is "off")
	if decision.DesiredState == PowerStateOff {
		blocks := EvaluateGuardrails(target, config)
		if len(blocks) > 0 {
			// Check if restoration is always allowed (R07)
			if target.Snapshot != nil {
				// R07 does not apply here — we are trying to power DOWN, not restore.
				// Blocks apply.
			}
			decision.BlockReasons = blocks
		}
	}

	// Step 5: Determine divergence
	observedPowerState := PowerStateFromObserved(target.ObservedState, target.Ref.Kind)
	if observedPowerState != decision.DesiredState && !decision.IsBlocked() {
		decision.Divergent = true
	}

	// Step 6: Determine snapshot requirement
	if decision.DesiredState == PowerStateOff && !decision.IsBlocked() && target.Snapshot == nil {
		decision.SnapshotRequired = true
	}

	return decision
}

// ComputeDecisions computes decisions for all targets (batch).
// Pre-computes shared state (rule evaluation) for efficiency.
func ComputeDecisions(targets []Target, policies []PolicySpec, overrides []OverrideSpec, config GuardrailConfig, now time.Time) []Decision {
	decisions := make([]Decision, 0, len(targets))
	for _, target := range targets {
		decisions = append(decisions, ComputeDecision(target, policies, overrides, config, now))
	}
	return decisions
}

// ResolvePriority determines the winning rule among competing rules.
// Rules must be non-empty.
// Tie-breaking order: priority (desc) → safety-first (on wins) → specificity (desc) → createdAt (desc)
func ResolvePriority(rules []EvaluatedRule) (winner EvaluatedRule, suppressed []EvaluatedRule) {
	if len(rules) == 0 {
		return EvaluatedRule{}, nil
	}

	if len(rules) == 1 {
		return rules[0], nil
	}

	// Sort by: priority desc, then specificity desc, then createdAt desc
	sorted := make([]EvaluatedRule, len(rules))
	copy(sorted, rules)

	slices.SortFunc(sorted, func(a, b EvaluatedRule) int {
		// Higher priority first
		if a.Ref.Priority != b.Ref.Priority {
			if a.Ref.Priority > b.Ref.Priority {
				return -1
			}
			return 1
		}
		// Same priority: check safety-first
		if a.EffectiveState != b.EffectiveState {
			if a.EffectiveState == PowerStateOn {
				return -1 // "on" wins (safety-first)
			}
			return 1
		}
		// Same priority, same state: most specific wins
		if a.Specificity != b.Specificity {
			if a.Specificity > b.Specificity {
				return -1
			}
			return 1
		}
		// Same priority, same state, same specificity: most recently created wins
		if !a.Ref.CreatedAt.Equal(b.Ref.CreatedAt) {
			if a.Ref.CreatedAt.After(b.Ref.CreatedAt) {
				return -1
			}
			return 1
		}
		// Final deterministic tiebreaker: alphabetical name
		if a.Ref.Name < b.Ref.Name {
			return -1
		}
		if a.Ref.Name > b.Ref.Name {
			return 1
		}
		return 0
	})

	return sorted[0], sorted[1:]
}

// collectApplicableRules gathers and evaluates all rules that apply to a target.
func collectApplicableRules(target Target, policies []PolicySpec, overrides []OverrideSpec, now time.Time) []EvaluatedRule {
	var rules []EvaluatedRule

	// Evaluate policies
	for _, policy := range policies {
		if !MatchesScope(target, policy.Scope) {
			continue
		}
		effectiveState := EvaluateSchedule(policy.Schedule, now)
		specificity := ComputeSpecificity(policy.Scope)
		rules = append(rules, EvaluatedRule{
			Ref: RuleRef{
				Kind:        RuleKindPolicy,
				Name:        policy.Name,
				Namespace:   policy.Namespace,
				Priority:    policy.Priority,
				Specificity: specificity,
				Description: policy.Description,
				CreatedAt:   policy.CreatedAt,
			},
			EffectiveState: effectiveState,
			Specificity:    specificity,
		})
	}

	// Evaluate overrides (only active ones)
	for _, override := range overrides {
		if override.IsExpired(now) {
			continue // R11: expired overrides are inert
		}
		if !MatchesScope(target, override.Scope) {
			continue
		}
		specificity := ComputeSpecificity(override.Scope)
		rules = append(rules, EvaluatedRule{
			Ref: RuleRef{
				Kind:        RuleKindOverride,
				Name:        override.Name,
				Namespace:   override.Namespace,
				Priority:    override.Priority,
				Specificity: specificity,
				Description: override.Reason,
				CreatedAt:   override.CreatedAt,
			},
			EffectiveState: override.State,
			Specificity:    specificity,
		})
	}

	return rules
}
