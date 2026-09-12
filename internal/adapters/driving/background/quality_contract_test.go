package background

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	return s
}

func TestQualityDiscoveryMigratesLegacyIdentityAndClearsSnapshotOnNewUID(t *testing.T) {
	replicas := int32(3)
	legacy := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "fixtures--api", Namespace: "aura-system"},
		Spec:       v1alpha1.PowerTargetSpec{TargetRef: v1alpha1.TargetReference{Namespace: "fixtures", Name: "api", Kind: "Deployment"}},
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
