package background

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
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
