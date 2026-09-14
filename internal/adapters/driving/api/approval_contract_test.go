package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/driven/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type approvalAuditFailingClient struct {
	client.Client
	fail bool
}

func (c *approvalAuditFailingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.fail {
		if _, ok := obj.(*v1alpha1.PowerAuditEvent); ok {
			return errors.New("injected audit failure")
		}
	}
	return c.Client.Create(ctx, obj, opts...)
}

type finalizeFailingStore struct{ auth.Store }

func (finalizeFailingStore) FinalizePendingDecision(string, string, string) (*auth.PendingChange, error) {
	return nil, errors.New("injected finalize failure")
}

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

func TestPendingApprovalDefaultsPayloadNamespaceToControlNamespace(t *testing.T) {
	f := newContractFixture(t)
	payload := pendingPolicyPayload("member-policy", v1alpha1.PowerPolicySpec{})
	payload["metadata"].(map[string]any)["namespace"] = ""
	created := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending", f.token(t, auth.RoleMember), map[string]any{
		"action": "create", "resourceKind": "PowerPolicy", "resourceName": "member-policy", "payload": payload,
	})
	if created.Code != 201 {
		t.Fatalf("create request: %d %s", created.Code, created.Body.String())
	}
	change := decodeContract[auth.PendingChange](t, created)
	approved := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+change.ID+"/approve", f.token(t, auth.RoleApprover), nil)
	if approved.Code != 200 {
		t.Fatalf("approve namespace-defaulted payload: %d %s", approved.Code, approved.Body.String())
	}
	var policy v1alpha1.PowerPolicy
	if err := f.client.Get(context.Background(), client.ObjectKey{Namespace: "aura-system", Name: "member-policy"}, &policy); err != nil {
		t.Fatalf("approved member policy was not created in control namespace: %v", err)
	}
}

func TestFailedApprovalPreservesPendingState(t *testing.T) {
	f := newContractFixture(t)
	pending, err := f.createPending(t, auth.PendingChange{
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
	if got.Status != "pending" || got.ReviewedAt != nil || got.ReviewedBy != "" {
		t.Fatalf("failed apply changed decision: %+v", got)
	}
	if rejected := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/reject", f.token(t, auth.RoleAdmin), nil); rejected.Code != 200 {
		t.Fatalf("failed approval could not be reclaimed: %d %s", rejected.Code, rejected.Body.String())
	}
}

func TestStaleUpdateRemainsPending(t *testing.T) {
	live := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "aura-system", ResourceVersion: "2"}}
	f := newContractFixture(t, live)
	payload, err := json.Marshal(pendingPolicyPayload("nightly", v1alpha1.PowerPolicySpec{Priority: 10}))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.createPending(t, auth.PendingChange{
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
	if got.Status != "pending" || got.ReviewedBy != "" || got.ReviewedAt != nil {
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
	claims, err := f.jwt.ValidateToken(token, auth.TokenTypeAccess)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateUser(claims.UserID, auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	pending, err := f.createPending(t, auth.PendingChange{
		UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy",
		ResourceNamespace: "aura-system", ResourceName: "denied", Payload: mustJSON(t, pendingPolicyPayload("denied", v1alpha1.PowerPolicySpec{})),
	})
	if err != nil {
		t.Fatal(err)
	}
	response := requestContract(t, f.server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/approve", token, nil)
	if response.Code != 401 {
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

func TestAppliedApprovalCannotBeReversedWhenAuditFails(t *testing.T) {
	f := newContractFixture(t)
	wrapped := &approvalAuditFailingClient{Client: f.client, fail: true}
	server := NewServer(wrapped, nil, f.server.config)
	server.RegisterAuthRoutes(f.store, f.jwt)
	server.FinalizeRoutes()
	pending, err := f.createPending(t, auth.PendingChange{Action: "create", ResourceKind: "PowerPolicy", ResourceNamespace: "aura-system", ResourceName: "audit-window", Payload: mustJSON(t, pendingPolicyPayload("audit-window", v1alpha1.PowerPolicySpec{}))})
	if err != nil {
		t.Fatal(err)
	}
	response := requestContract(t, server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/approve", f.token(t, auth.RoleApprover), nil)
	if response.Code != 422 {
		t.Fatalf("audit failure returned %d: %s", response.Code, response.Body.String())
	}
	var policy v1alpha1.PowerPolicy
	if err := f.client.Get(context.Background(), client.ObjectKey{Namespace: "aura-system", Name: "audit-window"}, &policy); err != nil {
		t.Fatalf("approved mutation was not applied: %v", err)
	}
	got, _ := f.store.GetPendingChange(pending.ID)
	if got.Status != "approving" {
		t.Fatalf("applied mutation became reversible: %+v", got)
	}
	if rejected := requestContract(t, server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/reject", f.token(t, auth.RoleAdmin), nil); rejected.Code != 409 {
		t.Fatalf("applied approval was reversed: %d %s", rejected.Code, rejected.Body.String())
	}
}

func TestAppliedAndAuditedApprovalCannotBeReversedWhenFinalizeFails(t *testing.T) {
	f := newContractFixture(t)
	server := NewServer(f.client, nil, f.server.config)
	server.RegisterAuthRoutes(finalizeFailingStore{Store: f.store}, f.jwt)
	server.FinalizeRoutes()
	pending, err := f.createPending(t, auth.PendingChange{Action: "create", ResourceKind: "PowerPolicy", ResourceNamespace: "aura-system", ResourceName: "finalize-window", Payload: mustJSON(t, pendingPolicyPayload("finalize-window", v1alpha1.PowerPolicySpec{}))})
	if err != nil {
		t.Fatal(err)
	}
	response := requestContract(t, server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/approve", f.token(t, auth.RoleApprover), nil)
	if response.Code != 500 {
		t.Fatalf("finalize failure returned %d: %s", response.Code, response.Body.String())
	}
	got, _ := f.store.GetPendingChange(pending.ID)
	if got.Status != "approving" {
		t.Fatalf("finalize failure made applied mutation reversible: %+v", got)
	}
	if rejected := requestContract(t, server.Handler(), "POST", "/api/v1/pending/"+pending.ID+"/reject", f.token(t, auth.RoleAdmin), nil); rejected.Code != 409 {
		t.Fatalf("applied approval was reversed after finalize failure: %d %s", rejected.Code, rejected.Body.String())
	}
}

func TestApprovalReplayPreservesOriginalDecisionOwner(t *testing.T) {
	f := newContractFixture(t)
	original, err := f.store.CreateUser("original-reviewer", "Original-reviewer-passphrase", auth.RoleApprover)
	if err != nil {
		t.Fatal(err)
	}
	retrying, err := f.store.CreateUser("retrying-reviewer", "Retrying-reviewer-passphrase", auth.RoleApprover)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.createPending(t, auth.PendingChange{
		UserID: "requester", Username: "member", Action: "create", ResourceKind: "PowerPolicy",
		ResourceNamespace: "aura-system", ResourceName: "owned-replay", Payload: mustJSON(t, pendingPolicyPayload("owned-replay", v1alpha1.PowerPolicySpec{})),
	})
	if err != nil {
		t.Fatal(err)
	}
	change, err := f.store.BeginPendingDecision(pending.ID, original.ID, "approve")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewAuthHandlers(f.store, f.jwt, f.client)
	if err := handler.applyPendingChange(context.Background(), change); err != nil {
		t.Fatal(err)
	}
	if err := handler.recordApprovalDecision(context.Background(), change, original, true); err != nil {
		t.Fatal(err)
	}

	owner := handler.decisionOwner(change, retrying)
	if owner.ID != original.ID || owner.Username != original.Username {
		t.Fatalf("retry changed durable decision owner: %+v", owner)
	}
	if err := handler.applyPendingChange(context.Background(), change); err != nil {
		t.Fatalf("applied mutation was not replayable: %v", err)
	}
	if err := handler.recordApprovalDecision(context.Background(), change, owner, true); err != nil {
		t.Fatalf("durable audit was not replayable: %v", err)
	}
	finalized, err := f.store.FinalizePendingDecision(change.ID, owner.ID, "approve")
	if err != nil || finalized.Status != "approved" || finalized.ReviewedBy != original.ID {
		t.Fatalf("replay did not finalize original decision: %+v %v", finalized, err)
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
