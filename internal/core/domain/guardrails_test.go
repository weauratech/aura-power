package domain

import "testing"

func TestEvaluateGuardrails_NoBlocks(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if blocks != nil {
		t.Fatalf("expected no blocks, got %d", len(blocks))
	}
}

func TestEvaluateGuardrails_SystemNamespace(t *testing.T) {
	target := makeTarget("kube-system", "coredns", WorkloadKindDeployment)
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != BlockSystemNamespace {
		t.Fatalf("expected BlockSystemNamespace, got %s", blocks[0].Type)
	}
	if blocks[0].Waivable {
		t.Fatal("system namespace block should NOT be waivable")
	}
}

func TestEvaluateGuardrails_SystemNamespaceNotWaivableEvenWithOptIn(t *testing.T) {
	target := makeTarget("kube-system", "coredns", WorkloadKindDeployment)
	target.Annotations = map[string]string{"aura.sh/power-eligible": "true"}
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block even with opt-in, got %d", len(blocks))
	}
}

func TestEvaluateGuardrails_ArgoCDManaged_Blocked(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Ownership = []OwnershipSignal{{Type: OwnershipArgoCD, OptedIn: false}}
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != BlockArgoCDManaged {
		t.Fatalf("expected BlockArgoCDManaged, got %s", blocks[0].Type)
	}
	if !blocks[0].Waivable {
		t.Fatal("ArgoCD block should be waivable")
	}
}

func TestEvaluateGuardrails_ArgoCDManaged_OptedIn(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Ownership = []OwnershipSignal{{Type: OwnershipArgoCD, OptedIn: false}}
	target.Annotations = map[string]string{"aura.sh/power-eligible": "true"}
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if blocks != nil {
		t.Fatalf("expected no blocks when opted in, got %d: %v", len(blocks), blocks)
	}
}

func TestEvaluateGuardrails_MultipleBlocks_AllReported(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Ownership = []OwnershipSignal{
		{Type: OwnershipArgoCD, OptedIn: false},
		{Type: OwnershipHPA, OptedIn: false},
	}
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (all reported), got %d", len(blocks))
	}
}

func TestEvaluateGuardrails_ExemptTargetReturnsNil(t *testing.T) {
	target := makeTarget("dev", "api", WorkloadKindDeployment)
	target.Annotations = map[string]string{"aura.sh/power-exempt": "true"}
	target.Ownership = []OwnershipSignal{{Type: OwnershipArgoCD, OptedIn: false}}
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if blocks != nil {
		t.Fatal("exempt target should return nil regardless of ownership")
	}
}

func TestDetectOwnership_ArgoCD(t *testing.T) {
	annotations := map[string]string{"argocd.argoproj.io/managed-by": "argocd"}
	labels := map[string]string{}

	signals := DetectOwnership(annotations, labels, "aura.sh/power-eligible")
	if len(signals) != 1 || signals[0].Type != OwnershipArgoCD {
		t.Fatal("expected ArgoCD ownership signal")
	}
}

func TestDetectOwnership_Helm(t *testing.T) {
	annotations := map[string]string{}
	labels := map[string]string{"app.kubernetes.io/managed-by": "Helm"}

	signals := DetectOwnership(annotations, labels, "aura.sh/power-eligible")
	if len(signals) != 1 || signals[0].Type != OwnershipHelm {
		t.Fatal("expected Helm ownership signal")
	}
}

func TestDetectOwnership_None(t *testing.T) {
	annotations := map[string]string{}
	labels := map[string]string{"app": "api"}

	signals := DetectOwnership(annotations, labels, "aura.sh/power-eligible")
	if len(signals) != 0 {
		t.Fatalf("expected no ownership signals, got %d", len(signals))
	}
}

func TestIsSystemNamespace_CustomBlocklist(t *testing.T) {
	config := DefaultGuardrailConfig()
	config.CustomBlocklist = []string{"monitoring", "istio-system"}

	if !IsSystemNamespace("monitoring", config) {
		t.Fatal("expected monitoring to be blocked via custom blocklist")
	}
	if IsSystemNamespace("dev", config) {
		t.Fatal("dev should not be blocked")
	}
}

func TestEvaluateGuardrails_AuraSystemNamespace(t *testing.T) {
	target := makeTarget("aura-system", "aura-power-controller", WorkloadKindDeployment)
	config := DefaultGuardrailConfig()

	blocks := EvaluateGuardrails(target, config)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block for aura-system namespace, got %d", len(blocks))
	}
	if blocks[0].Type != BlockSystemNamespace {
		t.Fatalf("expected BlockSystemNamespace, got %s", blocks[0].Type)
	}
}
