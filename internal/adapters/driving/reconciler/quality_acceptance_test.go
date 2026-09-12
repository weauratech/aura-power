//go:build acceptance

package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

func TestAcceptanceCTRL04PolicyNamespaceGroupsReachDomain(t *testing.T) {
	p := &v1alpha1.PowerPolicy{Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{NamespaceGroups: []string{"non-production"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	got := toDomainPolicy(p)
	// The public CRD advertises namespaceGroups, so conversion must retain a selector
	// rather than degrading the policy to an empty, cluster-wide scope.
	if len(got.Scope.Namespaces) == 0 && len(got.Scope.NamespaceGroups) == 0 && len(got.Scope.NamespaceLabels) == 0 && len(got.Scope.WorkloadNames) == 0 && len(got.Scope.WorkloadLabels) == 0 {
		t.Fatalf("CTRL-04: namespaceGroups was silently dropped: CRD=%+v domain=%+v", p.Spec.Scope, got.Scope)
	}
}

func TestAcceptanceCTRL05WorkloadAndNamespaceLabelsReachDecisionEngine(t *testing.T) {
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system", Labels: map[string]string{
		"power.aura.sh/target-namespace": "fixtures", "power.aura.sh/target-name": "api", "power.aura.sh/target-kind": "Deployment",
	}}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}}, Status: v1alpha1.PowerTargetStatus{
		WorkloadLabels: map[string]string{"tier": "backend"}, NamespaceLabels: map[string]string{"environment": "test"},
	}}
	got := toDomainTarget(target)
	if got.Labels["tier"] != "backend" || got.NamespaceLabels["environment"] != "test" {
		t.Fatalf("CTRL-05: discovery metadata cannot survive in PowerTarget: labels=%v namespaceLabels=%v", got.Labels, got.NamespaceLabels)
	}
}

func TestAcceptanceCTRL06StatusConflictCannotLoseSnapshotAndRepeatMutation(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}}, Status: v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3, PowerState: "on"}}}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	base := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	wrapped := &statusFailingClient{Client: base}
	executor := &countingExecutor{}
	r := TargetReconciler{Client: wrapped, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "aura-system", Name: "fixtures--api"}}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if executor.calls != 0 {
		t.Fatalf("CTRL-06: mutation executed %d times before snapshot status persisted", executor.calls)
	}
}

type statusFailingClient struct{ client.Client }

func (c *statusFailingClient) Status() client.SubResourceWriter { return failingStatusWriter{} }

type failingStatusWriter struct{}

func (failingStatusWriter) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	return errors.New("injected status failure")
}
func (failingStatusWriter) Update(context.Context, client.Object, ...client.SubResourceUpdateOption) error {
	return errors.New("injected status failure")
}
func (failingStatusWriter) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return errors.New("injected status failure")
}

type countingExecutor struct{ calls int }

func (*countingExecutor) CaptureSnapshot(context.Context, domain.WorkloadRef) (*domain.Snapshot, error) {
	replicas := int32(3)
	return &domain.Snapshot{ReplicaCount: &replicas}, nil
}
func (e *countingExecutor) PowerDown(context.Context, domain.WorkloadRef) error {
	e.calls++
	return nil
}
func (*countingExecutor) Restore(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	return nil
}

type noopAudit struct{}

func (noopAudit) Record(context.Context, ports.AuditEvent) error { return nil }
func (noopAudit) List(context.Context, ports.AuditListOptions) ([]ports.AuditEvent, error) {
	return nil, nil
}

type noopMetrics struct{}

func (noopMetrics) RecordReconciliation(time.Duration, error)             {}
func (noopMetrics) RecordAction(ports.ActionType, string, bool)           {}
func (noopMetrics) SetGauge(ports.MetricName, float64, map[string]string) {}
