//go:build acceptance

package api

import (
	"context"
	"testing"

	"github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These tests encode the public behavior promised by the approval API. They are
// deliberately kept behind the acceptance build tag so a confirmed product gap
// remains executable without making the fast unit suite permanently red.
func TestAcceptanceApprovedPolicyIsApplied(t *testing.T) {
	f := newContractFixture(t)
	approverToken := f.token(t, auth.RoleApprover)

	pending, err := f.createPending(t, auth.PendingChange{
		Action:       "create",
		ResourceKind: "PowerPolicy",
		ResourceName: "approved-policy",
		Payload: `{
			"apiVersion":"power.aura.sh/v1alpha1",
			"kind":"PowerPolicy",
			"metadata":{"name":"approved-policy","namespace":"aura-system"},
			"spec":{"scope":{"namespaces":["acceptance"]},"schedule":{"desiredState":"off"}}
		}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	response := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/approve", approverToken, nil)
	if response.Code != 200 {
		t.Fatalf("approval returned %d: %s", response.Code, response.Body.String())
	}

	var policy v1alpha1.PowerPolicy
	key := client.ObjectKey{Namespace: "aura-system", Name: "approved-policy"}
	if err := f.client.Get(context.Background(), key, &policy); err != nil {
		t.Fatalf("approval was acknowledged but the policy was not applied: %v", err)
	}
}

func TestAcceptanceRoleChangeRejectsUnsupportedRole(t *testing.T) {
	f := newContractFixture(t)
	adminToken := f.token(t, auth.RoleAdmin)
	user, err := f.store.CreateUser("member", auth.GenerateID(), auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}

	response := requestContract(t, f.server.Handler(), "PUT", "/api/v1/users/"+user.ID, adminToken, map[string]string{"role": "owner"})
	if response.Code != 400 {
		t.Fatalf("unsupported role was accepted: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAcceptanceExplainDisambiguatesWorkloadKind(t *testing.T) {
	deployment := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "shared-deployment",
			Namespace: "aura-system",
			Labels: map[string]string{
				"power.aura.sh/target-namespace": "acceptance",
				"power.aura.sh/target-name":      "shared",
			},
		},
		Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "acceptance", Name: "shared", Kind: "Deployment"}},
	}
	statefulSet := deployment.DeepCopy()
	statefulSet.Name = "shared-statefulset"
	statefulSet.Spec.TargetRef.Kind = "StatefulSet"
	f := newContractFixture(t, deployment, statefulSet)

	response := requestContract(t, f.server.Handler(), "GET", "/api/v1/targets/acceptance/shared/explain?kind=StatefulSet", f.token(t, auth.RoleMember), nil)
	if response.Code != 200 {
		t.Fatalf("explain returned %d: %s", response.Code, response.Body.String())
	}
	result := decodeContract[map[string]any](t, response)
	ref := result["ref"].(map[string]any)
	if ref["kind"] != "StatefulSet" {
		t.Fatalf("requested StatefulSet but explain returned %v", ref["kind"])
	}
}
