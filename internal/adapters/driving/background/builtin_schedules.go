package background

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

// DefaultTimezone is the default timezone for built-in schedules.
// Configurable via BUILT_IN_SCHEDULE_TIMEZONE env var (default: America/Sao_Paulo).
var DefaultTimezone = "America/Sao_Paulo"

// SeedBuiltInSchedules creates the built-in PowerSchedule resources if they don't exist.
func SeedBuiltInSchedules(ctx context.Context, c client.Client, namespace string) {
	log := ctrl.Log.WithName("builtin-schedules")

	schedules := []v1alpha1.PowerSchedule{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "business-hours",
				Namespace: namespace,
				Labels:    map[string]string{"power.aura.sh/built-in": "true"},
			},
			Spec: v1alpha1.PowerScheduleSpec{
				DesiredState: "on",
				Windows: []v1alpha1.TimeWindowSpec{
					{Start: "08:00", End: "18:00", Days: []int{1, 2, 3, 4, 5}, Timezone: DefaultTimezone},
				},
				Description: "Keep workloads on during business hours (Mon-Fri 08:00-18:00). Off otherwise.",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "always-off",
				Namespace: namespace,
				Labels:    map[string]string{"power.aura.sh/built-in": "true"},
			},
			Spec: v1alpha1.PowerScheduleSpec{
				DesiredState: "off",
				Description:  "Keep workloads off 24/7. Useful for deprecated namespaces.",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "weekdays-only",
				Namespace: namespace,
				Labels:    map[string]string{"power.aura.sh/built-in": "true"},
			},
			Spec: v1alpha1.PowerScheduleSpec{
				DesiredState: "on",
				Windows: []v1alpha1.TimeWindowSpec{
					{Start: "00:00", End: "23:59", Days: []int{1, 2, 3, 4, 5}, Timezone: DefaultTimezone},
				},
				Description: "Keep workloads on Mon-Fri only. Off on weekends.",
			},
		},
	}

	for _, s := range schedules {
		key := types.NamespacedName{Namespace: s.Namespace, Name: s.Name}
		var existing v1alpha1.PowerSchedule
		if err := c.Get(ctx, key, &existing); err == nil {
			// Already exists, skip
			continue
		}
		if err := c.Create(ctx, &s); err != nil {
			log.Error(err, "failed to seed built-in schedule", "name", s.Name)
		} else {
			log.Info("seeded built-in schedule", "name", s.Name)
		}
	}
}
