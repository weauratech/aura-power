package background

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

func TestSeedBuiltInSchedulesFreshInstallAndRestartAreIdempotent(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).Build()

	if err := SeedBuiltInSchedules(ctx, c, "aura-system"); err != nil {
		t.Fatalf("fresh install seed failed: %v", err)
	}

	var schedules v1alpha1.PowerScheduleList
	if err := c.List(ctx, &schedules, client.InNamespace("aura-system")); err != nil {
		t.Fatal(err)
	}
	if len(schedules.Items) != 3 {
		t.Fatalf("expected three built-in schedules, got %d", len(schedules.Items))
	}
	states := map[string]string{}
	for _, schedule := range schedules.Items {
		states[schedule.Name] = schedule.Spec.DesiredState
	}
	if states["business-hours"] != v1alpha1.PowerScheduleStateOn ||
		states["always-off"] != v1alpha1.PowerScheduleStateOff ||
		states["weekdays-only"] != v1alpha1.PowerScheduleStateOn {
		t.Fatalf("unexpected built-in states: %#v", states)
	}

	// Simulate an operator customization before a controller restart/upgrade.
	var existing v1alpha1.PowerSchedule
	if err := c.Get(ctx, client.ObjectKey{Namespace: "aura-system", Name: "always-off"}, &existing); err != nil {
		t.Fatal(err)
	}
	existing.Spec.Description = "operator-owned description"
	if err := c.Update(ctx, &existing); err != nil {
		t.Fatal(err)
	}

	if err := SeedBuiltInSchedules(ctx, c, "aura-system"); err != nil {
		t.Fatalf("restart seed failed: %v", err)
	}
	if err := c.Get(ctx, client.ObjectKey{Namespace: "aura-system", Name: "always-off"}, &existing); err != nil {
		t.Fatal(err)
	}
	if existing.Spec.Description != "operator-owned description" {
		t.Fatalf("restart overwrote an existing schedule: %q", existing.Spec.Description)
	}
}

type getErrorClient struct {
	client.Client
	err error
}

func (c getErrorClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return c.err
}

func TestSeedBuiltInSchedulesDoesNotCreateAfterReadFailure(t *testing.T) {
	ctx := context.Background()
	base := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).Build()
	wantErr := errors.New("temporary API failure")

	err := SeedBuiltInSchedules(ctx, getErrorClient{Client: base, err: wantErr}, "aura-system")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected read failure to be returned, got %v", err)
	}

	var schedules v1alpha1.PowerScheduleList
	if err := base.List(ctx, &schedules); err != nil {
		t.Fatal(err)
	}
	if len(schedules.Items) != 0 {
		t.Fatalf("created %d schedules after an inconclusive read", len(schedules.Items))
	}
}

func TestSeedBuiltInSchedulesLeavesExistingObjectUntouched(t *testing.T) {
	existing := &v1alpha1.PowerSchedule{
		ObjectMeta: metav1.ObjectMeta{Name: "business-hours", Namespace: "control"},
		Spec: v1alpha1.PowerScheduleSpec{
			DesiredState: v1alpha1.PowerScheduleStateOff,
			Description:  "custom",
		},
	}
	c := fake.NewClientBuilder().WithScheme(discoveryScheme(t)).WithObjects(existing).Build()
	if err := SeedBuiltInSchedules(context.Background(), c, "control"); err != nil {
		t.Fatal(err)
	}
	var got v1alpha1.PowerSchedule
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(existing), &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.DesiredState != v1alpha1.PowerScheduleStateOff || got.Spec.Description != "custom" {
		t.Fatalf("existing schedule was overwritten: %+v", got.Spec)
	}
}
