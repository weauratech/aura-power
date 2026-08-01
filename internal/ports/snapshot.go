package ports

import (
	"context"

	"github.com/weauratech/aura-power/internal/core/domain"
)

// SnapshotStore provides access to workload state snapshots.
type SnapshotStore interface {
	Save(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error
	Get(ctx context.Context, ref domain.WorkloadRef) (*domain.Snapshot, error)
	Delete(ctx context.Context, ref domain.WorkloadRef) error
	Exists(ctx context.Context, ref domain.WorkloadRef) (bool, error)
}
