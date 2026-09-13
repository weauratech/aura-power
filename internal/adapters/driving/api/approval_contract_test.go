package api

import (
	"context"
	"encoding/json"
	"testing"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func pendingPolicyPayload(name string, spec v1alpha1.PowerPolicySpec) map[string]any {
	return map[string]any{
		"apiVersion": "power.aura.sh/v1alpha1",
		"kind":       "PowerPolicy",
		"metadata":   map[string]any{"name": name, "namespace": "aura-system"},
		"spec":       spec,
	}
}

func TestPendingApprovalLifecycleAppliesAndCannotRepeat(t *testing.T) {
	f := newContractFixture(t)
	memberToken := f.token(t, auth.RoleMember)
	approverToken := f.token(t, auth.RoleApprover)
	request := map[string]any{
		"action": "create", "resourceKind": "PowerPolicy", "resourceNamespace": "aura-system", "resourceName": "nightly",
		"payload": pendingPolicyPayload("nightly", v1alpha1.PowerPolicySpec{}),
	}
	created := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending", memberToken, request)
	if created.Code != 201 {
		t.Fatalf("create request: %d %s", created.Code, created.Body.String())
	}
	change := decodeContract[auth.PendingChange](t, created)
	approved := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+change.ID+"/approve", approverToken, nil)
	if approved.Code != 200 {
		t.Fatalf("approve: %d %s", approved.Code, approved.Body.String())
	}
	var policy v1alpha1.PowerPolicy
	if err := f.client.Get(context.Background(), client.ObjectKey{Namespace: "aura-system", Name: "nightly"}, &policy); err != nil {
		t.Fatalf("approved policy was not created: %v", err)
	}
	var audit v1alpha1.PowerAuditEvent
	if err := f.client.Get(context.Background(), client.ObjectKey{Namespace: "aura-system", Name: "approval-" + change.ID}, &audit); err != nil {
		t.Fatalf("approval audit was not persisted: %v", err)
	}
	if audit.Spec.Actor == "" || audit.Spec.Target.Kind != "PowerPolicy" || audit.Spec.Action != "policy.created" {
		t.Fatalf("incomplete approval audit: %+v", audit.Spec)
	}
	repeated := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+change.ID+"/approve", approverToken, nil)
	if repeated.Code != 409 {
		t.Fatalf("repeat approval: got %d want 409", repeated.Code)
	}
	detail := requestContract(t, f.server.Handler(), "GET", "/api/v1/pending/"+change.ID, approverToken, nil)
	got := decodeContract[auth.PendingChange](t, detail)
	if got.Status != "approved" || got.ReviewedBy == "" || got.ReviewedAt == nil {
		t.Fatalf("approval audit fields missing: %+v", got)
	}
}

func TestFailedApprovalPreservesPendingState(t *testing.T) {
	f := newContractFixture(t)
	pending, err := f.store.CreatePendingChange(auth.PendingChange{
		UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy",
		ResourceNamespace: "aura-system", ResourceName: "broken", Payload: `{not-json}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/approve", f.token(t, auth.RoleApprover), nil)
	if response.Code != 422 {
		t.Fatalf("failure returned %d: %s", response.Code, response.Body.String())
	}
	got, err := f.store.GetPendingChange(pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "approving" || got.ReviewedAt != nil {
		t.Fatalf("failed apply changed decision: %+v", got)
	}
}

func TestStaleUpdateRemainsPending(t *testing.T) {
	live := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "aura-system", ResourceVersion: "2"}}
	f := newContractFixture(t, live)
	payload, err := json.Marshal(pendingPolicyPayload("nightly", v1alpha1.PowerPolicySpec{Priority: 10}))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.store.CreatePendingChange(auth.PendingChange{
		UserID: "requester", Username: "member", Action: "update", ResourceKind: "PowerPolicy",
		ResourceNamespace: "aura-system", ResourceName: "nightly", ResourceVersion: "1", Payload: string(payload),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/approve", f.token(t, auth.RoleApprover), nil)
	if response.Code != 409 {
		t.Fatalf("stale update returned %d: %s", response.Code, response.Body.String())
	}
	got, _ := f.store.GetPendingChange(pending.ID)
	if got.Status != "approving" {
		t.Fatalf("stale request status=%s", got.Status)
	}
	var unchanged v1alpha1.PowerPolicy
	if err := f.client.Get(context.Background(), client.ObjectKeyFromObject(live), &unchanged); err != nil {
		t.Fatal(err)
	}
	if unchanged.Spec.Priority != 0 {
		t.Fatal("stale request modified resource")
	}
}

func TestDowngradedReviewerCannotDecide(t *testing.T) {
	f := newContractFixture(t)
	token := f.token(t, auth.RoleApprover)
	claims, err := f.jwt.ValidateToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateUser(claims.UserID, auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	pending, err := f.store.CreatePendingChange(auth.PendingChange{
		UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy",
		ResourceNamespace: "aura-system", ResourceName: "denied", Payload: mustJSON(t, pendingPolicyPayload("denied", v1alpha1.PowerPolicySpec{})),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/approve", token, nil)
	if response.Code != 403 {
		t.Fatalf("downgraded reviewer got %d", response.Code)
	}
	got, _ := f.store.GetPendingChange(pending.ID)
	if got.Status != "pending" {
		t.Fatalf("downgrade changed status=%s", got.Status)
	}
}

func TestApprovedMutationCanBeReplayedAfterRestart(t *testing.T) {
	f := newContractFixture(t)
	payload := mustJSON(t, pendingPolicyPayload("replay", v1alpha1.PowerPolicySpec{Priority: 7}))
	change := &auth.PendingChange{Action: "create", ResourceKind: "PowerPolicy", ResourceNamespace: "aura-system", ResourceName: "replay", Payload: payload}
	handler := NewAuthHandlers(f.store, f.jwt, f.client)
	if err := handler.applyPendingChange(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	// Models a crash after Kubernetes succeeded but before SQLite was finalized.
	restarted := NewAuthHandlers(f.store, f.jwt, f.client)
	if err := restarted.applyPendingChange(context.Background(), change); err != nil {
		t.Fatalf("replay: %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
