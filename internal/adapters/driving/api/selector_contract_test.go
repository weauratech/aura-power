package api

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
)

func TestPreviewUsesResolvedGroupsAndAllSelectors(t *testing.T) {
	group := &v1alpha1.PowerNamespaceGroup{ObjectMeta: metav1.ObjectMeta{Name: "non-production", Namespace: "aura-system"}, Spec: v1alpha1.PowerNamespaceGroupSpec{Namespaces: []string{"dev", "staging"}}}
	target := func(objectName, namespace, name, tier, environment string) *v1alpha1.PowerTarget {
		return &v1alpha1.PowerTarget{
			ObjectMeta: metav1.ObjectMeta{Name: objectName, Namespace: "aura-system"},
			Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: namespace, Name: name, Kind: "Deployment"}},
			Status:     v1alpha1.PowerTargetStatus{WorkloadLabels: map[string]string{"tier": tier}, NamespaceLabels: map[string]string{"environment": environment}},
		}
	}
	f := newContractFixture(t, group,
		target("dev-api", "dev", "api", "backend", "test"),
		target("dev-worker", "dev", "worker", "backend", "test"),
		target("prod-api", "prod", "api", "backend", "prod"),
		target("staging-api", "staging", "api", "frontend", "test"),
	)
	token := f.token(t, auth.RoleMember)
	body := map[string]any{
		"scope": map[string]any{
			"namespaceGroups": []string{"non-production"},
			"namespaceLabels": map[string]string{"environment": "test"},
			"workloadNames":   []string{"api"},
			"workloadLabels":  map[string]string{"tier": "backend"},
		},
		"schedule": map[string]string{"desiredState": "off"},
	}
	response := requestContract(t, f.server.Handler(), "POST", "/api/v1/preview/policy", token, body)
	if response.Code != 200 {
		t.Fatalf("preview failed: %d %s", response.Code, response.Body.String())
	}
	got := decodeContract[map[string]int](t, response)
	if got["totalAffected"] != 1 || got["affectedOff"] != 1 {
		t.Fatalf("preview selected the wrong set: %v", got)
	}
}

func TestPreviewRejectsUnresolvedNamespaceGroup(t *testing.T) {
	f := newContractFixture(t)
	response := requestContract(t, f.server.Handler(), "POST", "/api/v1/preview/policy", f.token(t, auth.RoleMember), map[string]any{
		"scope":    map[string]any{"namespaceGroups": []string{"missing"}},
		"schedule": map[string]string{"desiredState": "off"},
	})
	if response.Code != 422 {
		t.Fatalf("unresolved group became executable: %d %s", response.Code, response.Body.String())
	}
}
