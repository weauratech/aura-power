// Package ports defines the interfaces between core domain and external adapters.
package ports

import (
	"context"

	"github.com/weauratech/aura-power/internal/core/domain"
)

// TargetRepository provides access to PowerTarget resources.
type TargetRepository interface {
	Get(ctx context.Context, namespace, name string) (*domain.Target, error)
	List(ctx context.Context, opts TargetListOptions) ([]domain.Target, error)
	UpdateStatus(ctx context.Context, ref domain.WorkloadRef, decision domain.Decision) error
}

// TargetListOptions provides filtering for target listing.
type TargetListOptions struct {
	Namespace string
	State     *domain.PowerState
	Blocked   *bool
	Divergent *bool
}

// PolicyRepository provides access to PowerPolicy resources.
type PolicyRepository interface {
	List(ctx context.Context) ([]domain.PolicySpec, error)
	Get(ctx context.Context, namespace, name string) (*domain.PolicySpec, error)
}

// OverrideRepository provides access to PowerOverride resources.
type OverrideRepository interface {
	List(ctx context.Context) ([]domain.OverrideSpec, error)
	ListActive(ctx context.Context) ([]domain.OverrideSpec, error)
	Get(ctx context.Context, namespace, name string) (*domain.OverrideSpec, error)
}

// ScheduleRepository provides access to PowerSchedule resources.
type ScheduleRepository interface {
	Get(ctx context.Context, name string) (*domain.Schedule, error)
	List(ctx context.Context) (map[string]domain.Schedule, error)
}
