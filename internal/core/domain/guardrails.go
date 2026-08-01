package domain

import "strings"

// EvaluateGuardrails checks if a target is blocked from power-down.
// Returns ALL matching block reasons (no short-circuit).
// Returns nil if the target is eligible.
func EvaluateGuardrails(target Target, config GuardrailConfig) []BlockReason {
	if IsExempt(target, config) {
		return nil // Exempt targets are excluded from all evaluation
	}

	var reasons []BlockReason

	if block := checkSystemNamespace(target, config); block != nil {
		reasons = append(reasons, *block)
	}

	ownershipBlocks := checkOwnership(target, config)
	reasons = append(reasons, ownershipBlocks...)

	// Filter out waivable blocks if target has opt-in
	if IsOptedIn(target, config) {
		var nonWaivable []BlockReason
		for _, r := range reasons {
			if !r.Waivable {
				nonWaivable = append(nonWaivable, r)
			}
		}
		return nonWaivable // Only non-waivable blocks remain
	}

	if len(reasons) == 0 {
		return nil
	}
	return reasons
}

// IsExempt returns true if the target has the exempt annotation.
func IsExempt(target Target, config GuardrailConfig) bool {
	if target.Annotations == nil {
		return false
	}
	return target.Annotations[config.ExemptAnnotation] == "true"
}

// IsOptedIn returns true if the target has the opt-in annotation.
func IsOptedIn(target Target, config GuardrailConfig) bool {
	if target.Annotations == nil {
		return false
	}
	return target.Annotations[config.OptInAnnotation] == "true"
}

// IsSystemNamespace checks if a namespace is in the protected list.
func IsSystemNamespace(namespace string, config GuardrailConfig) bool {
	for _, ns := range config.SystemNamespaces {
		if ns == namespace {
			return true
		}
	}
	for _, ns := range config.CustomBlocklist {
		if ns == namespace {
			return true
		}
	}
	return false
}

func checkSystemNamespace(target Target, config GuardrailConfig) *BlockReason {
	if IsSystemNamespace(target.Ref.Namespace, config) {
		return &BlockReason{
			Type:     BlockSystemNamespace,
			Message:  "System namespace (" + target.Ref.Namespace + ") is protected. Power-down is not allowed.",
			Waivable: false, // System namespace blocks are NEVER waivable
		}
	}
	return nil
}

func checkOwnership(target Target, config GuardrailConfig) []BlockReason {
	var blocks []BlockReason

	for _, signal := range target.Ownership {
		if signal.OptedIn {
			continue // Already opted in at the ownership signal level
		}

		switch signal.Type {
		case OwnershipArgoCD:
			blocks = append(blocks, BlockReason{
				Type:     BlockArgoCDManaged,
				Message:  "Managed by Argo CD. Add annotation " + config.OptInAnnotation + "=\"true\" to enable power management.",
				Waivable: true,
			})
		case OwnershipFlux:
			blocks = append(blocks, BlockReason{
				Type:     BlockFluxManaged,
				Message:  "Managed by Flux. Add annotation " + config.OptInAnnotation + "=\"true\" to enable power management.",
				Waivable: true,
			})
		case OwnershipHelm:
			blocks = append(blocks, BlockReason{
				Type:     BlockHelmManaged,
				Message:  "Managed by Helm. Add annotation " + config.OptInAnnotation + "=\"true\" to enable power management.",
				Waivable: true,
			})
		case OwnershipHPA:
			blocks = append(blocks, BlockReason{
				Type:     BlockHPAControlled,
				Message:  "Controlled by HPA. Scaling to 0 would conflict with autoscaling. Add annotation " + config.OptInAnnotation + "=\"true\" to override.",
				Waivable: true,
			})
		}
	}

	return blocks
}

// DetectOwnership detects external ownership signals from annotations and labels.
// nsAnnotations optionally provides the namespace annotations for inheriting opt-in.
func DetectOwnership(annotations, labels map[string]string, optInAnnotation string, nsAnnotations ...map[string]string) []OwnershipSignal {
	var signals []OwnershipSignal
	// Opt-in: check workload annotation first, then namespace annotation
	optedIn := annotations[optInAnnotation] == "true"
	if !optedIn && len(nsAnnotations) > 0 && nsAnnotations[0] != nil {
		optedIn = nsAnnotations[0][optInAnnotation] == "true"
	}

	// Argo CD detection
	if hasArgoCDSignals(annotations, labels) {
		signals = append(signals, OwnershipSignal{Type: OwnershipArgoCD, OptedIn: optedIn})
	}

	// Flux detection
	if hasFluxSignals(annotations, labels) {
		signals = append(signals, OwnershipSignal{Type: OwnershipFlux, OptedIn: optedIn})
	}

	// Helm detection
	if hasHelmSignals(labels) {
		signals = append(signals, OwnershipSignal{Type: OwnershipHelm, OptedIn: optedIn})
	}

	return signals
}

func hasArgoCDSignals(annotations, labels map[string]string) bool {
	for key := range annotations {
		if strings.HasPrefix(key, "argocd.argoproj.io/") {
			return true
		}
	}
	for key := range labels {
		if strings.HasPrefix(key, "argocd.argoproj.io/") {
			return true
		}
	}
	return false
}

func hasFluxSignals(annotations, labels map[string]string) bool {
	for key := range labels {
		if strings.HasPrefix(key, "kustomize.toolkit.fluxcd.io/") || strings.HasPrefix(key, "fluxcd.io/") {
			return true
		}
	}
	for key := range annotations {
		if strings.HasPrefix(key, "kustomize.toolkit.fluxcd.io/") || strings.HasPrefix(key, "fluxcd.io/") {
			return true
		}
	}
	return false
}

func hasHelmSignals(labels map[string]string) bool {
	return labels["app.kubernetes.io/managed-by"] == "Helm"
}
