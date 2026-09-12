package reconciler

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
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
