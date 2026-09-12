//go:build acceptance

package background

import (
	"context"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

func TestAcceptanceCTRL03HomonymousKindsHaveDistinctPowerTargets(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithStatusSubresource(&v1alpha1.PowerTarget{}).Build()
	loop := DiscoveryLoop{Client: c, Config: DiscoveryConfig{Namespace: "aura-system", ExemptAnnotation: "aura.sh/power-exempt", OptInAnnotation: "aura.sh/power-eligible"}}
	for _, kind := range []domain.WorkloadKind{domain.WorkloadKindDeployment, domain.WorkloadKindStatefulSet, domain.WorkloadKindCronJob} {
		_, err := loop.ensurePowerTarget(context.Background(), ports.DiscoveredWorkload{Ref: domain.WorkloadRef{Namespace: "fixtures", Name: "same", Kind: kind}})
		if err != nil {
			t.Fatal(err)
		}
	}
	var targets v1alpha1.PowerTargetList
	if err := c.List(context.Background(), &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets.Items) != 3 {
		t.Fatalf("CTRL-03: kind is absent from target identity; expected 3 targets, got %d (%+v)", len(targets.Items), targets.Items)
	}
}
