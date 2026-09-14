package background

import (
	"context"
	"errors"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

func discoveryScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestQualityDiscoveryMigratesLegacyIdentityAndClearsSnapshotOnNewUID(t *testing.T) {
	replicas := int32(3)
	legacy := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment", UID: "uid-one"}},
		Status: v1alpha1.PowerTargetStatus{
			Snapshot:            &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
			Action:              &v1alpha1.PowerActionStatus{DesiredState: "off", Phase: "Converged"},
			ConsecutiveFailures: 3,
			Savings:             &v1alpha1.SavingsSpec{CPUHoursSaved: 12},
		},
	}
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(legacy).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt", OptInAnnotation: "aura.sh/power-eligible"}}
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-one"}
	if _, err := loop.ensurePowerTarget(context.Background(), ports.DiscoveredWorkload{Ref: ref, Replicas: 3}); err != nil {
		t.Fatal(err)
	}
	var migrated v1alpha1.PowerTarget
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Status.Snapshot == nil || *migrated.Status.Snapshot.ReplicaCount != 3 || migrated.Spec.TargetRef.UID != "uid-one" {
		t.Fatalf("legacy state was not migrated safely: %+v", migrated)
	}
	ref.UID = "uid-two"
	if _, err := loop.ensurePowerTarget(context.Background(), ports.DiscoveredWorkload{Ref: ref, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Status.Snapshot != nil || migrated.Status.Action != nil || migrated.Status.ConsecutiveFailures != 0 || migrated.Status.Savings != nil || migrated.Spec.TargetRef.UID != "uid-two" {
		t.Fatalf("recreated workload inherited stale state: %+v", migrated)
	}
}

func TestQualityDiscoveryBindsLegacySnapshotForPoweredDownWorkload(t *testing.T) {
	replicas := int32(3)
	now := metav1.Now()
	legacy := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}},
		Status:     v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"}, Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas, CapturedAt: &now}},
	}
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(legacy).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt", OptInAnnotation: "aura.sh/power-eligible"}}
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-current"}
	if _, err := loop.ensurePowerTarget(context.Background(), ports.DiscoveredWorkload{Ref: ref, Replicas: 0}); err != nil {
		t.Fatal(err)
	}
	var migrated v1alpha1.PowerTarget
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Status.Snapshot == nil || migrated.Status.Snapshot.ReplicaCount == nil || *migrated.Status.Snapshot.ReplicaCount != 3 || migrated.Spec.TargetRef.UID != "uid-current" {
		t.Fatalf("powered-down legacy workload lost its only recovery snapshot: %+v", migrated)
	}
}

func TestQualityDiscoveryResumesLegacyMigrationAfterStatusFailure(t *testing.T) {
	replicas := int32(3)
	now := metav1.Now()
	legacy := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}},
		Status:     v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"}, Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas, CapturedAt: &now}},
	}
	base := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(legacy).Build()
	flaky := &discoveryStatusFailingClient{Client: base, fail: true}
	loop := DiscoveryLoop{Client: flaky, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt"}}
	wl := ports.DiscoveredWorkload{Ref: domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-current"}, Replicas: 0}
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err == nil {
		t.Fatal("initial status failure was not surfaced")
	}
	flaky.fail = false
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatalf("partial migration was not resumed: %v", err)
	}
	var migrated v1alpha1.PowerTarget
	if err := base.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(wl.Ref)}, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Status.Snapshot == nil || migrated.Status.Snapshot.ReplicaCount == nil || *migrated.Status.Snapshot.ReplicaCount != replicas {
		t.Fatalf("resumed migration lost recovery snapshot: %+v", migrated.Status)
	}
	if err := base.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: legacy.Name}, &v1alpha1.PowerTarget{}); err == nil {
		t.Fatal("legacy identity remained after resumed migration")
	}
}

func TestQualityDiscoveryDropsAmbiguousLegacySnapshotForRunningWorkload(t *testing.T) {
	replicas := int32(3)
	now := metav1.Now()
	legacy := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}},
		Status:     v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"}, Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas, CapturedAt: &now}},
	}
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(legacy).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt", OptInAnnotation: "aura.sh/power-eligible"}}
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-current"}
	if _, err := loop.ensurePowerTarget(context.Background(), ports.DiscoveredWorkload{Ref: ref, Replicas: 1}); err != nil {
		t.Fatal(err)
	}
	var migrated v1alpha1.PowerTarget
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated.Status.Snapshot != nil {
		t.Fatalf("running workload inherited an ambiguous legacy snapshot: %+v", migrated.Status)
	}
}

func TestLegacySnapshotRequiresRecoveryProvenance(t *testing.T) {
	replicas := int32(3)
	legacy := &v1alpha1.PowerTarget{Status: v1alpha1.PowerTargetStatus{
		ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"},
		Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas},
	}}
	wl := ports.DiscoveredWorkload{Ref: domain.WorkloadRef{Kind: domain.WorkloadKindDeployment}, Replicas: 0}
	if legacySnapshotProvesPoweredDown(legacy, wl) {
		t.Fatal("legacy snapshot without capture time was trusted")
	}
	now := metav1.Now()
	legacy.Status.Snapshot.CapturedAt = &now
	legacy.Status.ObservedState.PowerState = ""
	if legacySnapshotProvesPoweredDown(legacy, wl) {
		t.Fatal("legacy snapshot without an observed off state was trusted")
	}
}

func TestQualityLegacyZeroReplicaSnapshotIsAmbiguous(t *testing.T) {
	zero := int32(0)
	now := metav1.Now()
	legacy := &v1alpha1.PowerTarget{Status: v1alpha1.PowerTargetStatus{
		ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"},
		Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &zero, CapturedAt: &now},
	}}
	wl := ports.DiscoveredWorkload{Ref: domain.WorkloadRef{Kind: domain.WorkloadKindDeployment}, Replicas: 0}
	if legacySnapshotProvesPoweredDown(legacy, wl) {
		t.Fatal("ambiguous v2.1 zero snapshot was trusted")
	}
}

func TestQualityDiscoveryRetainsAmbiguousLegacyZeroUntilOperatorRecovery(t *testing.T) {
	zero := int32(0)
	now := metav1.Now()
	legacyName := "fixtures--api"
	legacy := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: legacyName, Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}},
		Status:     v1alpha1.PowerTargetStatus{ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"}, Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &zero, CapturedAt: &now}},
	}
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(legacy).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt"}}
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-current"}
	if _, err := loop.ensurePowerTarget(context.Background(), ports.DiscoveredWorkload{Ref: ref, Replicas: 0}); err == nil {
		t.Fatal("ambiguous zero snapshot was reported as migrated")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: legacyName}, &v1alpha1.PowerTarget{}); err != nil {
		t.Fatalf("ambiguous recovery record was discarded: %v", err)
	}
	if _, err := loop.ensurePowerTarget(context.Background(), ports.DiscoveredWorkload{Ref: ref, Replicas: 2}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: legacyName}, &v1alpha1.PowerTarget{}); err == nil {
		t.Fatal("legacy record remained after verified operator recovery")
	}
}

func TestQualityDiscoverySkipsSystemAndExemptWorkloads(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", SystemNamespaces: []string{"kube-system"}, ExemptAnnotation: "aura.sh/power-exempt", OptInAnnotation: "aura.sh/power-eligible"}}
	for _, wl := range []ports.DiscoveredWorkload{
		{Ref: domain.WorkloadRef{Namespace: "kube-system", Name: "dns", Kind: domain.WorkloadKindDeployment}},
		{Ref: domain.WorkloadRef{Namespace: "fixtures", Name: "excluded", Kind: domain.WorkloadKindDeployment}, Annotations: map[string]string{"aura.sh/power-exempt": "true"}},
	} {
		created, err := loop.ensurePowerTarget(context.Background(), wl)
		if err != nil || created {
			t.Fatalf("unsafe target admitted: created=%v err=%v", created, err)
		}
	}
	var targets v1alpha1.PowerTargetList
	if err := c.List(context.Background(), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets.Items) != 0 {
		t.Fatalf("created %d protected targets", len(targets.Items))
	}
}

func TestQualityDiscoveryRequiresLeaderElection(t *testing.T) {
	loop := &DiscoveryLoop{}
	if !loop.NeedLeaderElection() {
		t.Fatal("discovery and orphan cleanup must run only on the elected leader")
	}
}

func TestQualityDiscoveryPersistsHPAOwnershipAndOptInChanges(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt", OptInAnnotation: "aura.sh/power-eligible"}}
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-api"}
	wl := ports.DiscoveredWorkload{Ref: ref, Replicas: 2, HPAControlled: true}
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatal(err)
	}

	key := types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}
	var target v1alpha1.PowerTarget
	if err := c.Get(context.Background(), key, &target); err != nil {
		t.Fatal(err)
	}
	if len(target.Status.Ownership) != 1 || target.Status.Ownership[0].Type != string(domain.OwnershipHPA) || target.Status.Ownership[0].OptedIn {
		t.Fatalf("HPA ownership was not persisted as blocked: %+v", target.Status.Ownership)
	}

	wl.NamespaceAnnotations = map[string]string{"aura.sh/power-eligible": "true"}
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), key, &target); err != nil {
		t.Fatal(err)
	}
	if len(target.Status.Ownership) != 1 || !target.Status.Ownership[0].OptedIn {
		t.Fatalf("namespace opt-in did not update HPA ownership: %+v", target.Status.Ownership)
	}

	wl.HPAControlled = false
	wl.NamespaceAnnotations = nil
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), key, &target); err != nil {
		t.Fatal(err)
	}
	if len(target.Status.Ownership) != 0 {
		t.Fatalf("stale HPA ownership remained after HPA removal: %+v", target.Status.Ownership)
	}
}

func TestQualityNamespacePolicyCollisionFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name         string
		labels       map[string]string
		shouldUpdate bool
	}{
		{name: "user policy without ownership labels", labels: map[string]string{"owner": "user"}},
		{name: "namespace ownership label points elsewhere", labels: map[string]string{"power.aura.sh/source": "namespace-annotation", "power.aura.sh/namespace": "another-namespace"}},
		{name: "controller-owned policy", labels: map[string]string{"power.aura.sh/source": "namespace-annotation", "power.aura.sh/namespace": "team-a"}, shouldUpdate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a", Annotations: map[string]string{
				"aura.sh/default-schedule": "always-off", "aura.sh/power-priority": "321",
			}}}
			schedule := &v1alpha1.PowerSchedule{
				ObjectMeta: metav1.ObjectMeta{Name: "always-off", Namespace: "aura-system"},
				Spec:       v1alpha1.PowerScheduleSpec{DesiredState: "off", Windows: []v1alpha1.TimeWindowSpec{{Start: "00:00", End: "23:59", Timezone: "UTC"}}},
			}
			policy := &v1alpha1.PowerPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "ns-default-team-a", Namespace: "aura-system", Labels: tc.labels},
				Spec: v1alpha1.PowerPolicySpec{
					Scope:       v1alpha1.PolicyScope{Namespaces: []string{"user-owned-scope"}},
					Schedule:    v1alpha1.PolicySchedule{DesiredState: "on"},
					Priority:    999,
					Description: "user-owned sentinel",
				},
			}
			originalSpec := policy.DeepCopy().Spec
			originalLabels := copyStringMap(policy.Labels)
			c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithObjects(ns, schedule, policy).Build()
			loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system"}}
			loop.processNamespaceAnnotations(context.Background())

			var got v1alpha1.PowerPolicy
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(policy), &got); err != nil {
				t.Fatal(err)
			}
			if tc.shouldUpdate {
				if got.Spec.Priority != 321 || got.Spec.Schedule.DesiredState != "off" ||
					!reflect.DeepEqual(got.Spec.Schedule.Windows, schedule.Spec.Windows) ||
					!reflect.DeepEqual(got.Spec.Scope.Namespaces, []string{"team-a"}) ||
					got.Spec.Description != "Auto-generated from namespace team-a annotation (schedule: always-off)" {
					t.Fatalf("owned implicit policy was not reconciled: %+v", got.Spec)
				}
				return
			}
			if !reflect.DeepEqual(got.Spec, originalSpec) || !reflect.DeepEqual(got.Labels, originalLabels) {
				t.Fatalf("colliding user policy was mutated: before=%+v/%v after=%+v/%v", originalSpec, originalLabels, got.Spec, got.Labels)
			}
		})
	}
}

func TestQualityExemptionRestoresBeforeTargetDeletion(t *testing.T) {
	replicas := int32(3)
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-api"}
	target := &v1alpha1.PowerTarget{ObjectMeta: metav1.ObjectMeta{Name: powerTargetName(ref), Namespace: "aura-system"}, Spec: v1alpha1.PowerTargetSpec{TargetRef: targetReference(ref)}, Status: v1alpha1.PowerTargetStatus{Snapshot: &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas}}}
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(target).Build()
	executor := &exemptionExecutor{}
	loop := DiscoveryLoop{Client: c, Executor: executor, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt"}}
	wl := ports.DiscoveredWorkload{Ref: ref, Replicas: 0, Annotations: map[string]string{"aura.sh/power-exempt": "true"}}
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 {
		t.Fatalf("restore calls=%d", executor.restores)
	}
	var retained v1alpha1.PowerTarget
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}, &retained); err != nil {
		t.Fatalf("snapshot target deleted before observing recovery: %v", err)
	}
	wl.Replicas = 3
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}, &retained); err == nil {
		t.Fatal("target retained after restored workload became exempt")
	}
}

func TestQualityExemptionMigratesAndRestoresLegacySnapshot(t *testing.T) {
	replicas := int32(4)
	now := metav1.Now()
	ref := domain.WorkloadRef{APIVersion: "apps/v1", Namespace: "fixtures", Name: "legacy", Kind: domain.WorkloadKindDeployment, UID: "uid-current"}
	legacyName := "fixtures--legacy"
	legacy := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: legacyName, Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{APIVersion: "apps/v1", Namespace: "fixtures", Name: "legacy", Kind: "Deployment"}},
		Status: v1alpha1.PowerTargetStatus{
			ObservedState: v1alpha1.ObservedStateSpec{Replicas: 0, PowerState: "off"},
			Snapshot:      &v1alpha1.SnapshotSpec{Available: true, ReplicaCount: &replicas, CapturedAt: &now},
		},
	}
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).WithObjects(legacy).Build()
	executor := &exemptionExecutor{}
	loop := DiscoveryLoop{Client: c, Executor: executor, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt"}}
	wl := ports.DiscoveredWorkload{Ref: ref, Replicas: 0, Annotations: map[string]string{"aura.sh/power-exempt": "true"}}
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatal(err)
	}
	if executor.restores != 1 {
		t.Fatalf("legacy exempt workload was not restored: calls=%d", executor.restores)
	}
	var retained v1alpha1.PowerTarget
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: legacyName}, &retained); err != nil {
		t.Fatalf("legacy recovery record was not retained until restoration is observed: %v", err)
	}
	if retained.Status.Snapshot == nil {
		t.Fatal("legacy recovery snapshot was lost")
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(ref)}, &v1alpha1.PowerTarget{}); err == nil {
		t.Fatal("replacement target was created before restoration was observed")
	}
	wl.Replicas = replicas
	if _, err := loop.ensurePowerTarget(context.Background(), wl); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: legacyName}, &retained); err == nil {
		t.Fatal("legacy recovery record remained after restoration was observed")
	}
}

type exemptionExecutor struct{ restores int }

type discoveryStatusFailingClient struct {
	client.Client
	fail bool
}

func (c *discoveryStatusFailingClient) Status() client.SubResourceWriter {
	return &discoveryStatusFailingWriter{delegate: c.Client.Status(), owner: c}
}

type discoveryStatusFailingWriter struct {
	delegate client.SubResourceWriter
	owner    *discoveryStatusFailingClient
}

func (w *discoveryStatusFailingWriter) Create(ctx context.Context, obj, sub client.Object, opts ...client.SubResourceCreateOption) error {
	if w.owner.fail {
		return errors.New("injected migration status failure")
	}
	return w.delegate.Create(ctx, obj, sub, opts...)
}
func (w *discoveryStatusFailingWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if w.owner.fail {
		return errors.New("injected migration status failure")
	}
	return w.delegate.Update(ctx, obj, opts...)
}
func (w *discoveryStatusFailingWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if w.owner.fail {
		return errors.New("injected migration status failure")
	}
	return w.delegate.Patch(ctx, obj, patch, opts...)
}

func (*exemptionExecutor) CaptureSnapshot(context.Context, domain.WorkloadRef) (*domain.Snapshot, error) {
	return nil, nil
}
func (*exemptionExecutor) PowerDown(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	return nil
}
func (e *exemptionExecutor) Restore(context.Context, domain.WorkloadRef, domain.Snapshot) error {
	e.restores++
	return nil
}

func TestQualityDiscoveryPersistsSelectionLabels(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt", OptInAnnotation: "aura.sh/power-eligible"}}
	workload := ports.DiscoveredWorkload{
		Ref:             domain.WorkloadRef{Namespace: "fixtures", Name: "api", Kind: domain.WorkloadKindDeployment, UID: "uid-api"},
		Replicas:        2,
		Labels:          map[string]string{"tier": "backend", "eligible": ""},
		NamespaceLabels: map[string]string{"environment": "test"},
	}
	if _, err := loop.ensurePowerTarget(context.Background(), workload); err != nil {
		t.Fatal(err)
	}
	var target v1alpha1.PowerTarget
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(workload.Ref)}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Status.WorkloadLabels["tier"] != "backend" || target.Status.NamespaceLabels["environment"] != "test" {
		t.Fatalf("selection metadata lost: workload=%v namespace=%v", target.Status.WorkloadLabels, target.Status.NamespaceLabels)
	}
	workload.Labels = map[string]string{"tier": "worker"}
	workload.NamespaceLabels = map[string]string{"environment": "staging"}
	if _, err := loop.ensurePowerTarget(context.Background(), workload); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "aura-system", Name: powerTargetName(workload.Ref)}, &target); err != nil {
		t.Fatal(err)
	}
	if target.Status.WorkloadLabels["tier"] != "worker" || target.Status.NamespaceLabels["environment"] != "staging" {
		t.Fatalf("selection metadata was not refreshed: workload=%v namespace=%v", target.Status.WorkloadLabels, target.Status.NamespaceLabels)
	}
}
