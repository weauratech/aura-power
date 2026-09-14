package reconciler

import (
	"context"
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

func TestQualityCRDConversionPreservesCorePolicyContract(t *testing.T) {
	created := metav1.NewTime(time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC))
	p := &v1alpha1.PowerPolicy{ObjectMeta: metav1.ObjectMeta{Name: "off-hours", Namespace: "control", CreationTimestamp: created}, Spec: v1alpha1.PowerPolicySpec{
		Scope: v1alpha1.PolicyScope{TargetRefs: []v1alpha1.TargetReference{{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "api-uid"}},
			Namespaces: []string{"fixtures"}, NamespaceLabels: map[string]string{"team": "platform"}, WorkloadNames: []string{"api"}, WorkloadLabels: map[string]string{"tier": "backend"}},
		Schedule: v1alpha1.PolicySchedule{DesiredState: "off", Windows: []v1alpha1.TimeWindowSpec{{Start: "22:30", End: "06:15", Days: []int{1, 2}, Timezone: "UTC"}}}, Priority: 7, Description: "quality",
	}}
	got := toDomainPolicy(p)
	if got.Name != "off-hours" || got.Namespace != "control" || got.Priority != 7 || got.Schedule.DesiredState != domain.PowerStateOff || len(got.Schedule.Windows) != 1 {
		t.Fatalf("core fields lost: %+v", got)
	}
	if len(got.Scope.TargetRefs) != 1 || got.Scope.TargetRefs[0].Kind != domain.WorkloadKindDeployment || got.Scope.TargetRefs[0].UID != "api-uid" {
		t.Fatalf("exact target reference lost: %+v", got.Scope.TargetRefs)
	}
	w := got.Schedule.Windows[0]
	if w.Start.Hour != 22 || w.Start.Minute != 30 || w.End.Hour != 6 || w.End.Minute != 15 || w.Timezone != "UTC" {
		t.Fatalf("time conversion lost meaning: %+v", w)
	}
}

func TestQualityBeginActionCapturesLiveNamespaceNotificationPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "campaign", UID: "namespace-uid", Labels: map[string]string{ports.NotificationPolicyLabel: ports.NotificationPolicyDisabled}}}
	workload := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "fixture", Namespace: "campaign", UID: "workload-uid", Labels: map[string]string{ports.NotificationPolicyLabel: ports.NotificationPolicyDisabled}}}
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "campaign", Name: "fixture", Kind: "Deployment", UID: "workload-uid"}},
		// Cached labels deliberately disagree; only the live Namespace is authoritative.
		Status: v1alpha1.PowerTargetStatus{NamespaceLabels: map[string]string{ports.NotificationPolicyLabel: "enabled"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(namespace, workload, target).Build()
	r := &TargetReconciler{Client: c, APIReader: c}
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "campaign", Name: "fixture", Kind: domain.WorkloadKindDeployment, UID: "workload-uid"}
	decision := domain.Decision{DesiredState: domain.PowerStateOff}
	if err := r.beginAction(context.Background(), target, decision, ref, ports.AuditWorkloadPoweredDown, "quality"); err != nil {
		t.Fatal(err)
	}
	var persisted v1alpha1.PowerTarget
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(target), &persisted); err != nil {
		t.Fatal(err)
	}
	action := persisted.Status.Action
	if action == nil || !action.NotificationSuppressed || action.NotificationSuppressionSource != "namespace-label" || action.NotificationSuppressionNamespaceUID != "namespace-uid" {
		t.Fatalf("live disposition was not captured before mutation: %+v", action)
	}

	delete(namespace.Labels, ports.NotificationPolicyLabel)
	if err := c.Update(context.Background(), namespace); err != nil {
		t.Fatal(err)
	}
	if !persisted.Status.Action.NotificationSuppressed {
		t.Fatal("persisted action changed after the live Namespace label changed")
	}
	audit := &capturingAuditRecorder{}
	r.Audit = audit
	r.Metrics = contractNoopMetrics{}
	persisted.Status.Action.AuditPhase = "Pending"
	if handled, err := r.reconcilePendingAudit(context.Background(), &persisted, ref, "campaign/fixture"); err != nil || !handled {
		t.Fatalf("pending audit did not resume from captured action: handled=%v err=%v", handled, err)
	}
	if len(audit.events) != 1 || !audit.events[0].SuppressNotification || audit.events[0].NotificationSuppressionSource != "namespace-label" {
		t.Fatalf("namespace label change altered pending audit disposition: %+v", audit.events)
	}

	// Capture normal delivery while the namespace is enabled, then prove that a
	// later disabled label cannot retroactively suppress this action's audit.
	if err := r.beginAction(context.Background(), &persisted, decision, ref, ports.AuditWorkloadPoweredDown, "quality-enabled"); err != nil {
		t.Fatal(err)
	}
	if persisted.Status.Action.NotificationSuppressed {
		t.Fatalf("enabled namespace captured suppression: %+v", persisted.Status.Action)
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(namespace), namespace); err != nil {
		t.Fatal(err)
	}
	if namespace.Labels == nil {
		namespace.Labels = map[string]string{}
	}
	namespace.Labels[ports.NotificationPolicyLabel] = ports.NotificationPolicyDisabled
	if err := c.Update(context.Background(), namespace); err != nil {
		t.Fatal(err)
	}
	persisted.Status.Action.AuditPhase = "Pending"
	if handled, err := r.reconcilePendingAudit(context.Background(), &persisted, ref, "campaign/fixture"); err != nil || !handled {
		t.Fatalf("enabled pending audit did not resume: handled=%v err=%v", handled, err)
	}
	if len(audit.events) != 2 || audit.events[1].SuppressNotification {
		t.Fatalf("late namespace label changed captured delivery disposition: %+v", audit.events)
	}

	staleRef := ref
	staleRef.UID = "replacement-uid"
	if _, err := r.liveNotificationSuppression(context.Background(), target, staleRef); err == nil {
		t.Fatal("workload UID mismatch did not fail closed")
	}

	// A workload-level label is deliberately ignored. Only the live namespace
	// can suppress external notifications for a campaign.
	if namespace.Labels == nil {
		namespace.Labels = map[string]string{}
	}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(namespace), namespace); err != nil {
		t.Fatal(err)
	}
	delete(namespace.Labels, ports.NotificationPolicyLabel)
	if err := c.Update(context.Background(), namespace); err != nil {
		t.Fatal(err)
	}
	disposition, err := r.liveNotificationSuppression(context.Background(), target, ref)
	if err != nil {
		t.Fatal(err)
	}
	if disposition.Suppressed {
		t.Fatal("workload label overrode the live namespace authority")
	}
}

type capturingAuditRecorder struct {
	events []ports.AuditEvent
}

type contractNoopMetrics struct{}

func (contractNoopMetrics) RecordReconciliation(time.Duration, error)             {}
func (contractNoopMetrics) RecordAction(ports.ActionType, string, bool)           {}
func (contractNoopMetrics) SetGauge(ports.MetricName, float64, map[string]string) {}

func (r *capturingAuditRecorder) Record(_ context.Context, event ports.AuditEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (r *capturingAuditRecorder) List(context.Context, ports.AuditListOptions) ([]ports.AuditEvent, error) {
	return nil, nil
}

func TestQualityPreActionAuditIgnoresStaleActionAndFailsClosed(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "campaign", Name: "fixture", Kind: domain.WorkloadKindDeployment, UID: "workload-uid"}
	workload := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: ref.Name, Namespace: ref.Namespace, UID: "workload-uid"}}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ref.Namespace, UID: "namespace-uid", Labels: map[string]string{ports.NotificationPolicyLabel: ports.NotificationPolicyDisabled}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(workload, namespace).Build()
	audit := &capturingAuditRecorder{}
	r := &TargetReconciler{APIReader: c, Audit: audit}
	stale := &v1alpha1.PowerTarget{Status: v1alpha1.PowerTargetStatus{Action: &v1alpha1.PowerActionStatus{NotificationSuppressed: false}}}

	// The pre-beginAction call site passes nil so an unrelated prior Action can
	// never override the current live namespace disposition.
	r.recordAudit(context.Background(), nil, ref, ports.AuditExecutionError, "error", "snapshot failed", "")
	if len(audit.events) != 1 || !audit.events[0].SuppressNotification || audit.events[0].NotificationSuppressionSource != "namespace-label" || audit.events[0].NotificationSuppressionNamespaceUID != "namespace-uid" {
		t.Fatalf("pre-action audit reused stale or incomplete disposition: stale=%+v event=%+v", stale.Status.Action, audit.events)
	}

	// Any uncertainty at this boundary is suppressed rather than defaulting to
	// an external delivery that could escape the fixture.
	r.APIReader = fake.NewClientBuilder().WithScheme(scheme).Build()
	r.recordAudit(context.Background(), nil, ref, ports.AuditExecutionError, "error", "snapshot failed", "")
	if len(audit.events) != 2 || !audit.events[1].SuppressNotification || audit.events[1].NotificationSuppressionSource != "resolution-error" {
		t.Fatalf("resolution error did not fail closed: %+v", audit.events)
	}
}

func TestQualityBeginActionFailsClosedWithoutLiveReader(t *testing.T) {
	target := &v1alpha1.PowerTarget{Status: v1alpha1.PowerTargetStatus{NamespaceLabels: map[string]string{ports.NotificationPolicyLabel: ports.NotificationPolicyDisabled}}}
	r := &TargetReconciler{}
	err := r.beginAction(context.Background(), target, domain.Decision{DesiredState: domain.PowerStateOff}, domain.WorkloadRef{
		APIVersion: "apps/v1", Namespace: "campaign", Name: "fixture", Kind: domain.WorkloadKindDeployment, UID: "workload-uid",
	}, ports.AuditWorkloadPoweredDown, "quality")
	if err == nil || target.Status.Action != nil {
		t.Fatalf("missing live reader did not block intent before mutation: action=%+v err=%v", target.Status.Action, err)
	}
}

func TestQualityStableTargetStatusUsesBoundedCheckpoints(t *testing.T) {
	base := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	rule := &domain.RuleRef{Kind: domain.RuleKindPolicy, Name: "office-hours", Namespace: "aura-system"}
	decision := domain.Decision{DesiredState: domain.PowerStateOn, WinningRule: rule}
	target := &v1alpha1.PowerTarget{}

	updateTargetStatus(target, decision, base)
	checkpoint := target.DeepCopy().Status
	updateTargetStatus(target, decision, base.Add(30*time.Second))
	if !reflect.DeepEqual(checkpoint, target.Status) {
		t.Fatalf("stable reconcile changed status before checkpoint: before=%+v after=%+v", checkpoint, target.Status)
	}

	updateTargetStatus(target, decision, base.Add(statusCheckpointInterval))
	if target.Status.LastReconciliation == nil || !target.Status.LastReconciliation.Time.Equal(base.Add(statusCheckpointInterval)) {
		t.Fatalf("status checkpoint was not advanced: %+v", target.Status.LastReconciliation)
	}
}

func TestQualityStatusConversionPreservesSnapshotAndOwnership(t *testing.T) {
	replicas := int32(3)
	suspended := false
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "control", Labels: map[string]string{"tier": "backend"}, Annotations: map[string]string{"aura.sh/power-eligible": "true"}}, Spec: v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}}, Status: v1alpha1.PowerTargetStatus{
		ObservedState: v1alpha1.ObservedStateSpec{Replicas: 3}, Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas, Suspended: &suspended}, Ownership: []v1alpha1.OwnershipSpec{{Type: "ArgoCD", OptedIn: true}},
	}}
	got := toDomainTarget(target)
	if got.Ref.Kind != domain.WorkloadKindDeployment || got.Snapshot == nil || *got.Snapshot.ReplicaCount != 3 || len(got.Ownership) != 1 || !got.Ownership[0].OptedIn {
		t.Fatalf("target conversion lost state: %+v", got)
	}
}
