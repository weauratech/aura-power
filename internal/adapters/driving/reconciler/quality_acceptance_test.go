//go:build acceptance

package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	}}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}}, Status: v1alpha1.PowerTargetStatus{
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
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}}, Status: v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3, PowerState: "on"}}}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	base := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	wrapped := &statusFailingClient{Client: base}
	executor := &countingExecutor{}
	r := TargetReconciler{Client: wrapped, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
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

func TestAcceptanceSnapshotCaptureFailureIsPersisted(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}}, Status: v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3, PowerState: "on"}}}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: captureFailingExecutor{}, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}

	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.ConsecutiveFailures != 1 {
		t.Fatalf("snapshot capture failure was not persisted: %+v", got.Status)
	}
}

func TestAcceptanceRestoreWithoutSnapshotReturnsErrorAndDoesNotMutate(t *testing.T) {
	executor := &countingExecutor{}
	r := TargetReconciler{Executor: executor}
	target := &v1alpha1.PowerTarget{}
	if err := r.executeRestore(context.Background(), target, domain.WorkloadRef{Kind: domain.WorkloadKindDeployment}); err == nil {
		t.Fatal("restore without snapshot reported success")
	}
	if executor.restores != 0 {
		t.Fatalf("restore without snapshot mutated a workload %d times", executor.restores)
	}
}

func TestAcceptanceArgoContentionDoesNotRepeatPowerDown(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	replicas := int32(2)
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: replicas, PowerState: "on"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "aura-system", Name: "fixtures--api"}}

	// The fake workload observation deliberately remains at two replicas, as it
	// would when Argo CD self-heal immediately reclaims spec.replicas.
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var settling v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &settling); err != nil {
		t.Fatal(err)
	}
	settledAt := metav1.NewTime(time.Now().Add(-actionConvergenceGrace - time.Second))
	settling.Status.Action.CompletedAt = &settledAt
	if err := c.Status().Update(context.Background(), &settling); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := r.Reconcile(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if executor.calls != 1 {
		t.Fatalf("expected one power-down write under contention, got %d", executor.calls)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Action == nil || got.Status.Action.Phase != "Contended" {
		t.Fatalf("expected durable Contended action, got %+v", got.Status.Action)
	}
	if got.Annotations == nil {
		got.Annotations = map[string]string{}
	}
	got.Annotations[retryActionAnnotation] = "ownership-fixed-1"
	if err := c.Update(context.Background(), &got); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 2 {
		t.Fatalf("explicit retry token should authorize exactly one new write, got %d total", executor.calls)
	}
}

func TestAcceptanceActionIntentMustPersistBeforeMutation(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	replicas := int32(2)
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: replicas, PowerState: "on"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	base := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: &statusFailingClient{Client: base}, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "aura-system", Name: "fixtures--api"}}); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("mutation executed before durable action intent: %d", executor.calls)
	}
}

func TestAcceptanceStaleSnapshotIsRetiredBeforeSafeRecapture(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "snapshot-race", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status:     v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3, PowerState: "on"}},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &staleOnceExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}

	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var afterConflict v1alpha1.PowerTarget
	if err := c.Get(context.Background(), request.NamespacedName, &afterConflict); err != nil {
		t.Fatal(err)
	}
	if afterConflict.Status.Snapshot != nil || afterConflict.Status.Action == nil || afterConflict.Status.Action.Phase != "Failed" {
		t.Fatalf("stale snapshot remained eligible for retry: %+v", afterConflict.Status)
	}

	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var afterRetry v1alpha1.PowerTarget
	if err := c.Get(context.Background(), request.NamespacedName, &afterRetry); err != nil {
		t.Fatal(err)
	}
	if executor.captures != 2 || executor.downs != 2 || afterRetry.Status.Snapshot == nil || afterRetry.Status.Snapshot.ReplicaCount == nil || *afterRetry.Status.Snapshot.ReplicaCount != 5 || afterRetry.Status.Snapshot.ResourceVersion != "rv-new" {
		t.Fatalf("safe recapture did not use the concurrent revision: executor=%+v status=%+v", executor, afterRetry.Status)
	}
}

func TestAcceptanceMutationSuccessRequiresDurableCompletionCheckpoint(t *testing.T) {
	for _, desired := range []domain.PowerState{domain.PowerStateOff, domain.PowerStateOn} {
		t.Run(string(desired), func(t *testing.T) {
			s := runtime.NewScheme()
			if err := v1alpha1.AddToScheme(s); err != nil {
				t.Fatal(err)
			}
			replicas := int32(2)
			observed := v1alpha1.ObservedStateSpec{Replicas: 2, PowerState: "on"}
			status := v1alpha1.PowerTargetStatus{ObservedState: observed, Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas}}
			if desired == domain.PowerStateOn {
				status.ObservedState = v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"}
			}
			target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "checkpoint", Namespace: "aura-system"}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}}, Status: status}
			policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "rule", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: string(desired)}}}
			base := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
			wrapped := &statusFailingOnCallClient{Client: base, failOn: 2}
			executor := &countingExecutor{}
			metrics := &recordingMetrics{}
			audit := &recordingAudit{}
			r := TargetReconciler{Client: wrapped, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: audit, Metrics: metrics}
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}); err != nil {
				t.Fatal(err)
			}
			if executor.calls+executor.restores != 1 {
				t.Fatalf("mutation was not executed exactly once: powerDown=%d restore=%d", executor.calls, executor.restores)
			}
			if metrics.successes != 0 || metrics.failures != 1 {
				t.Fatalf("checkpoint failure reported as success: %+v", metrics)
			}
			for _, event := range audit.events {
				if event.Result == "success" {
					t.Fatalf("checkpoint failure emitted success audit: %+v", event)
				}
			}
			var persisted v1alpha1.PowerTarget
			if err := base.Get(context.Background(), client.ObjectKeyFromObject(target), &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.Status.Action == nil || persisted.Status.Action.Phase != "InProgress" {
				t.Fatalf("durable recovery intent was lost: %+v", persisted.Status.Action)
			}

			// Simulate discovery after a controller crash: the workload mutation
			// converged, while the completion status write above was lost. Recovery
			// must checkpoint and audit that outcome without issuing the mutation a
			// second time.
			if desired == domain.PowerStateOff {
				persisted.Status.ObservedState = v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"}
			} else {
				persisted.Status.ObservedState = v1alpha1.ObservedStateSpec{Replicas: replicas, PowerState: "on"}
			}
			if err := base.Status().Update(context.Background(), &persisted); err != nil {
				t.Fatal(err)
			}
			request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Reconcile(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			successAudits := 0
			for _, event := range audit.events {
				if event.Result == "success" {
					successAudits++
				}
			}
			if executor.calls+executor.restores != 1 || successAudits != 1 || metrics.successes != 1 {
				t.Fatalf("crash recovery repeated mutation or lost/duplicated success: powerDown=%d restore=%d successAudits=%d allAudits=%d metrics=%+v", executor.calls, executor.restores, successAudits, len(audit.events), metrics)
			}
			if err := base.Get(context.Background(), client.ObjectKeyFromObject(target), &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.Status.Action == nil || persisted.Status.Action.AuditPhase != "Recorded" {
				t.Fatalf("recovered audit checkpoint was not durable: %+v", persisted.Status.Action)
			}
		})
	}
}

func TestAcceptanceRestoreKeepsSnapshotUntilRunningStateIsObserved(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	replicas := int32(2)
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "restore-checkpoint", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "restore", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "on"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var persisted v1alpha1.PowerTarget
	if err := c.Get(context.Background(), request.NamespacedName, &persisted); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 || persisted.Status.Snapshot == nil || persisted.Status.Snapshot.ReplicaCount == nil || *persisted.Status.Snapshot.ReplicaCount != replicas {
		t.Fatalf("restore checkpoint discarded recovery evidence before observation: restores=%d status=%+v", executor.restores, persisted.Status)
	}
	if persisted.Status.Action == nil || persisted.Status.Action.Phase != "Applied" {
		t.Fatalf("restore acceptance checkpoint missing: %+v", persisted.Status.Action)
	}

	// Discovery confirms convergence; only this observation may retire the
	// recovery snapshot.
	persisted.Status.ObservedState = v1alpha1.ObservedStateSpec{Replicas: replicas, PowerState: "on"}
	if err := c.Status().Update(context.Background(), &persisted); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), request.NamespacedName, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status.Snapshot != nil || persisted.Status.Action == nil || persisted.Status.Action.Phase != "Converged" {
		t.Fatalf("snapshot was not retired after observed convergence: %+v", persisted.Status)
	}
}

func TestAcceptanceAuditFailureRetriesWithoutRepeatingMutation(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	replicas := int32(2)
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "audit-retry", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "restore-rule", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "on"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	audit := &failOnceAudit{failuresRemaining: 1}
	metrics := &recordingMetrics{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: audit, Metrics: metrics}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}

	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var pending v1alpha1.PowerTarget
	if err := c.Get(context.Background(), request.NamespacedName, &pending); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 || len(audit.events) != 0 || metrics.successes != 0 {
		t.Fatalf("audit failure produced false success or wrong mutation count: restores=%d audits=%d metrics=%+v", executor.restores, len(audit.events), metrics)
	}
	if pending.Status.Action == nil || pending.Status.Action.Phase != "Applied" || pending.Status.Action.AuditPhase != "Pending" || pending.Status.Action.AuditEventID == "" {
		t.Fatalf("pending audit checkpoint was not durable: %+v", pending.Status.Action)
	}

	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	var recorded v1alpha1.PowerTarget
	if err := c.Get(context.Background(), request.NamespacedName, &recorded); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 || len(audit.events) != 1 || metrics.successes != 1 {
		t.Fatalf("audit retry repeated mutation or missed success: restores=%d audits=%d metrics=%+v", executor.restores, len(audit.events), metrics)
	}
	event := audit.events[0]
	if event.ID != pending.Status.Action.AuditEventID || event.Action != ports.AuditWorkloadRestored || event.Actor != "system/controller" || event.RuleName != "restore-rule" || event.Target.UID != "uid-api" {
		t.Fatalf("retried audit lost accepted semantics: %+v", event)
	}
	if recorded.Status.Action == nil || recorded.Status.Action.AuditPhase != "Recorded" {
		t.Fatalf("recorded audit checkpoint was not persisted: %+v", recorded.Status.Action)
	}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 || len(audit.events) != 1 || metrics.successes != 1 {
		t.Fatalf("recorded audit was repeated: restores=%d audits=%d metrics=%+v", executor.restores, len(audit.events), metrics)
	}
}

func TestAcceptanceAuditCheckpointFailureDoesNotDuplicateSideEffects(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	replicas := int32(2)
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "audit-checkpoint-retry", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "restore-rule", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "on"}}}
	base := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	wrapped := &statusFailingOnCallClient{Client: base, failOn: 3}
	executor := &countingExecutor{}
	audit := &idempotentRecordingAudit{events: make(map[string]ports.AuditEvent)}
	metrics := &recordingMetrics{}
	r := TargetReconciler{Client: wrapped, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: audit, Metrics: metrics}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}

	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 || audit.calls != 1 || len(audit.events) != 1 || metrics.successes != 0 {
		t.Fatalf("failed recorded checkpoint leaked side effects: restores=%d calls=%d events=%d metrics=%+v", executor.restores, audit.calls, len(audit.events), metrics)
	}
	var persisted v1alpha1.PowerTarget
	if err := base.Get(context.Background(), request.NamespacedName, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status.Action == nil || persisted.Status.Action.AuditPhase != "Pending" {
		t.Fatalf("failed recorded checkpoint did not remain retryable: %+v", persisted.Status.Action)
	}

	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 || audit.calls != 2 || len(audit.events) != 1 || metrics.successes != 1 {
		t.Fatalf("checkpoint retry duplicated mutation, audit event, or metric: restores=%d calls=%d events=%d metrics=%+v", executor.restores, audit.calls, len(audit.events), metrics)
	}
}

func TestAcceptanceArgoRestoredLiveStateIsNotOverwrittenByStaleSnapshot(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	snapshotReplicas := int32(2)
	now := metav1.Now()
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 5, PowerState: "on"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &snapshotReplicas},
			Action:        &v1alpha1.PowerActionStatus{DesiredState: "off", Phase: "Converged", AttemptedAt: &now},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "on", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "on"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKey{Namespace: "aura-system", Name: "fixtures--api"}}

	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 0 {
		t.Fatalf("existing Git-restored state must not be overwritten, got %d restore writes", executor.restores)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Snapshot != nil || got.Status.Action == nil || got.Status.Action.DesiredState != "on" || got.Status.Action.Phase != "Converged" {
		t.Fatalf("expected the live on-state to supersede the stale snapshot: %+v", got.Status)
	}
}

func TestAcceptanceContendedLiveRestoreRetiresStaleSnapshot(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	snapshotReplicas := int32(2)
	now := metav1.Now()
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 1, PowerState: "on"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &snapshotReplicas},
			Action:        &v1alpha1.PowerActionStatus{DesiredState: "off", Phase: "Contended", AttemptedAt: &now},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "on", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "on"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 0 {
		t.Fatalf("contended live restore was overwritten %d times", executor.restores)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Snapshot != nil || got.Status.Action == nil || got.Status.Action.DesiredState != "on" || got.Status.Action.Phase != "Converged" {
		t.Fatalf("contended live state did not retire stale recovery state: %+v", got.Status)
	}
}

func TestAcceptancePolicyFlipPreservesSnapshotUntilPowerDownIsObserved(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	snapshotReplicas := int32(2)
	now := metav1.Now()
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 2, PowerState: "on"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &snapshotReplicas},
			Action:        &v1alpha1.PowerActionStatus{DesiredState: "off", Phase: "Applied", AttemptedAt: &now, CompletedAt: &now},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "on", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "on"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: &countingExecutor{}, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.Snapshot == nil || got.Status.Snapshot.ReplicaCount == nil || *got.Status.Snapshot.ReplicaCount != 2 {
		t.Fatalf("snapshot was discarded while discovery still held the pre-power-down observation: %+v", got.Status)
	}
}

func TestAcceptanceInvalidNamespaceGroupDoesNotBlockUnrelatedRestore(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	replicas := int32(2)
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
		},
	}
	valid := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "restore", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "on"}}}
	invalid := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "broken-group", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{NamespaceGroups: []string{"deleted"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, valid, invalid).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 {
		t.Fatalf("unresolved group blocked unrelated restore: restores=%d", executor.restores)
	}
}

func TestAcceptanceInProgressActionRetriesAfterCrash(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	replicas := int32(2)
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	decisionKey := powerDecisionKey(domain.Decision{DesiredState: domain.PowerStateOff, WinningRule: &domain.RuleRef{Kind: domain.RuleKindPolicy, Name: "off", Namespace: "aura-system"}})
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}}, Status: v1alpha1.PowerTargetStatus{
		ObservedState: v1alpha1.ObservedStateSpec{Replicas: 2, PowerState: "on"}, Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas}, Action: &v1alpha1.PowerActionStatus{DesiredState: "off", DecisionKey: decisionKey, Phase: "InProgress"},
	}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("InProgress intent was not recovered idempotently: calls=%d", executor.calls)
	}
}

func TestAcceptanceTargetWithoutUIDCannotMutateWorkload(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: "aura-system"}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}}, Status: v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 2, PowerState: "on"}}}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: acceptanceLiveReader{}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("UID-less target mutated workload %d times", executor.calls)
	}
}

func TestAcceptanceLiveHPAAddedBeforeMutationFailsClosedUntilDiscoveredOptIn(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := autoscalingv2.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "target-api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status:     v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3, PowerState: "on"}},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "fixtures"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
			APIVersion: "apps/v1", Kind: "Deployment", Name: "api",
		}},
	}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "fixtures"}}
	workload := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "fixtures", UID: "uid-api"}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy, hpa, namespace, workload).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: c, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("newly-created HPA raced discovery and allowed %d mutations", executor.calls)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Status.Blocked || len(got.Status.BlockReasons) != 1 || got.Status.BlockReasons[0].Type != string(domain.BlockHPAControlled) || len(got.Status.Ownership) != 1 || got.Status.Ownership[0].Type != string(domain.OwnershipHPA) || got.Status.Ownership[0].OptedIn {
		t.Fatalf("live HPA race was not persisted fail-closed: %+v", got.Status)
	}
	if got.Status.Snapshot == nil || !got.Status.Snapshot.Available {
		t.Fatalf("safe pre-mutation snapshot was not retained: %+v", got.Status.Snapshot)
	}

	// A persisted opt-in projection alone is insufficient: the workload and
	// namespace no longer carry the annotation at the mutation boundary.
	got.Status.Ownership[0].OptedIn = true
	if err := c.Status().Update(context.Background(), &got); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("removed live opt-in allowed %d mutations from stale ownership", executor.calls)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(workload), workload); err != nil {
		t.Fatal(err)
	}
	workload.Annotations = map[string]string{domain.DefaultGuardrailConfig().OptInAnnotation: "true"}
	if err := c.Update(context.Background(), workload); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	// Discovery subsequently persists the current opt-in projection.
	got.Status.Ownership[0].OptedIn = true
	if err := c.Status().Update(context.Background(), &got); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("current and discovered explicit HPA opt-in did not admit one mutation: calls=%d", executor.calls)
	}
}

func TestAcceptanceLiveHPAListFailureBlocksMutation(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := autoscalingv2.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "target-api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status:     v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3, PowerState: "on"}},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: &hpaListFailingReader{Reader: c}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("HPA LIST failure allowed %d mutations", executor.calls)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Status.Blocked || len(got.Status.BlockReasons) != 1 || got.Status.BlockReasons[0].Type != string(domain.BlockInsufficientInfo) || got.Status.BlockReasons[0].Waivable || got.Status.ConsecutiveFailures != 1 {
		t.Fatalf("HPA inspection failure was not persisted fail-closed: %+v", got.Status)
	}
}

func TestAcceptanceLiveHPAOptInGetFailureBlocksMutation(t *testing.T) {
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := autoscalingv2.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "target-api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-api"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3, PowerState: "on"},
			Ownership:     []v1alpha1.OwnershipSpec{{Type: string(domain.OwnershipHPA), OptedIn: true}},
		},
	}
	policy := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off", Namespace: "aura-system"}, Spec: v1alpha1.PowerPolicySpec{Scope: v1alpha1.PolicyScope{Namespaces: []string{"fixtures"}}, Schedule: v1alpha1.PolicySchedule{DesiredState: "off"}}}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "fixtures"}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "api"}}}
	c := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target, policy, hpa).Build()
	executor := &countingExecutor{}
	r := TargetReconciler{Client: c, APIReader: &hpaGetFailingReader{Reader: c}, Config: domain.DefaultGuardrailConfig(), Executor: executor, Audit: noopAudit{}, Metrics: noopMetrics{}}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatalf("live opt-in GET failure allowed %d mutations", executor.calls)
	}
	var got v1alpha1.PowerTarget
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Status.Blocked || len(got.Status.BlockReasons) != 1 || got.Status.BlockReasons[0].Type != string(domain.BlockInsufficientInfo) || got.Status.BlockReasons[0].Waivable {
		t.Fatalf("live opt-in GET failure was not persisted fail-closed: %+v", got.Status)
	}
}

type hpaListFailingReader struct{ client.Reader }

func (r *hpaListFailingReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if _, ok := list.(*autoscalingv2.HorizontalPodAutoscalerList); ok {
		return errors.New("injected HPA LIST failure")
	}
	return r.Reader.List(ctx, list, opts...)
}

type hpaGetFailingReader struct{ client.Reader }

func (r *hpaGetFailingReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("injected live opt-in GET failure")
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

type statusFailingOnCallClient struct {
	client.Client
	calls  int
	failOn int
}

func (c *statusFailingOnCallClient) Status() client.SubResourceWriter {
	return &statusFailingOnCallWriter{delegate: c.Client.Status(), parent: c}
}

type statusFailingOnCallWriter struct {
	delegate client.SubResourceWriter
	parent   *statusFailingOnCallClient
}

func (w *statusFailingOnCallWriter) shouldFail() bool {
	w.parent.calls++
	return w.parent.calls == w.parent.failOn
}
func (w *statusFailingOnCallWriter) Create(ctx context.Context, obj, sub client.Object, opts ...client.SubResourceCreateOption) error {
	if w.shouldFail() {
		return errors.New("injected post-mutation status failure")
	}
	return w.delegate.Create(ctx, obj, sub, opts...)
}
func (w *statusFailingOnCallWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if w.shouldFail() {
		return errors.New("injected post-mutation status failure")
	}
	return w.delegate.Update(ctx, obj, opts...)
}
func (w *statusFailingOnCallWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if w.shouldFail() {
		return errors.New("injected post-mutation status failure")
	}
	return w.delegate.Patch(ctx, obj, patch, opts...)
}

type countingExecutor struct {
	calls    int
	restores int
}

type staleOnceExecutor struct {
	captures int
	downs    int
}

func (e *staleOnceExecutor) CaptureSnapshot(context.Context, domain.WorkloadRef) (*domain.Snapshot, error) {
	e.captures++
	replicas := int32(3)
	version := "rv-old"
	if e.captures > 1 {
		replicas = 5
		version = "rv-new"
	}
	return &domain.Snapshot{ReplicaCount: &replicas, ResourceVersion: version}, nil
}

func (e *staleOnceExecutor) PowerDown(_ context.Context, _ domain.WorkloadRef, snapshot domain.Snapshot) error {
	e.downs++
	if e.downs == 1 {
		return ports.ErrSnapshotStale
	}
	if snapshot.ReplicaCount == nil || *snapshot.ReplicaCount != 5 || snapshot.ResourceVersion != "rv-new" {
		return errors.New("retry used stale snapshot")
	}
	return nil
}

func (*staleOnceExecutor) Restore(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	return nil
}

type captureFailingExecutor struct{}

func (captureFailingExecutor) CaptureSnapshot(context.Context, domain.WorkloadRef) (*domain.Snapshot, error) {
	return nil, errors.New("injected snapshot capture failure")
}
func (captureFailingExecutor) PowerDown(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	return nil
}
func (captureFailingExecutor) Restore(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	return nil
}

func (*countingExecutor) CaptureSnapshot(context.Context, domain.WorkloadRef) (*domain.Snapshot, error) {
	replicas := int32(3)
	return &domain.Snapshot{ReplicaCount: &replicas}, nil
}
func (e *countingExecutor) PowerDown(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	e.calls++
	return nil
}
func (e *countingExecutor) Restore(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	e.restores++
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

type recordingMetrics struct{ successes, failures int }

func (*recordingMetrics) RecordReconciliation(time.Duration, error) {}
func (m *recordingMetrics) RecordAction(_ ports.ActionType, _ string, success bool) {
	if success {
		m.successes++
	} else {
		m.failures++
	}
}
func (*recordingMetrics) SetGauge(ports.MetricName, float64, map[string]string) {}

type recordingAudit struct{ events []ports.AuditEvent }

func (a *recordingAudit) Record(_ context.Context, event ports.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}
func (*recordingAudit) List(context.Context, ports.AuditListOptions) ([]ports.AuditEvent, error) {
	return nil, nil
}

type failOnceAudit struct {
	failuresRemaining int
	events            []ports.AuditEvent
}

func (a *failOnceAudit) Record(_ context.Context, event ports.AuditEvent) error {
	if a.failuresRemaining > 0 {
		a.failuresRemaining--
		return errors.New("injected audit persistence failure")
	}
	a.events = append(a.events, event)
	return nil
}
func (*failOnceAudit) List(context.Context, ports.AuditListOptions) ([]ports.AuditEvent, error) {
	return nil, nil
}

type idempotentRecordingAudit struct {
	calls  int
	events map[string]ports.AuditEvent
}

func (a *idempotentRecordingAudit) Record(_ context.Context, event ports.AuditEvent) error {
	a.calls++
	if existing, ok := a.events[event.ID]; ok {
		if existing.Action != event.Action || existing.Result != event.Result || existing.RuleName != event.RuleName || existing.Target != event.Target {
			return errors.New("same audit ID has different semantics")
		}
		return nil
	}
	a.events[event.ID] = event
	return nil
}
func (*idempotentRecordingAudit) List(context.Context, ports.AuditListOptions) ([]ports.AuditEvent, error) {
	return nil, nil
}

// acceptanceLiveReader supplies the live workload/namespace boundary required
// by the reconciler while these focused saga tests keep Kubernetes execution in
// their purpose-built executor doubles.
type acceptanceLiveReader struct{}

func (acceptanceLiveReader) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	switch typed := obj.(type) {
	case *appsv1.Deployment:
		typed.Name = key.Name
		typed.Namespace = key.Namespace
		typed.UID = types.UID("uid-" + key.Name)
		return nil
	case *appsv1.StatefulSet:
		typed.Name = key.Name
		typed.Namespace = key.Namespace
		typed.UID = types.UID("uid-" + key.Name)
		return nil
	case *corev1.Namespace:
		typed.Name = key.Name
		typed.UID = "namespace-uid"
		return nil
	default:
		return errors.New("acceptance live reader received an unsupported object")
	}
}

func (acceptanceLiveReader) List(_ context.Context, _ client.ObjectList, _ ...client.ListOption) error {
	return nil
}
