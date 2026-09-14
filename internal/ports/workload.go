package ports

import (
	"context"
	"errors"

	"github.com/weauratech/aura-power/internal/core/domain"
)

// ErrSnapshotStale proves that no power-down was applied because the workload
// changed after snapshot capture. The reconciler may safely recapture it.
var ErrSnapshotStale = errors.New("workload changed after snapshot capture")

// DiscoveredWorkload represents a workload found during discovery.
type DiscoveredWorkload struct {
	Ref                  domain.WorkloadRef
	Replicas             int32
	Suspended            bool
	ActiveJobs           int32
	Annotations          map[string]string
	Labels               map[string]string
	NamespaceLabels      map[string]string
	NamespaceAnnotations map[string]string
	Resources            domain.ResourceSummary
	// HPAControlled is set when an autoscaling/v2 HorizontalPodAutoscaler in
	// the workload namespace targets this exact API version, kind, and name.
	// Discovery carries this separately from metadata-derived ownership because
	// the signal lives on another Kubernetes object.
	HPAControlled bool
}

// WorkloadDiscoverer discovers Kubernetes workloads in the cluster.
type WorkloadDiscoverer interface {
	DiscoverAll(ctx context.Context, namespaces []string) ([]DiscoveredWorkload, error)
	DiscoverByNamespace(ctx context.Context, namespace string) ([]DiscoveredWorkload, error)
}

// WorkloadExecutor performs power actions on workloads.
type WorkloadExecutor interface {
	CaptureSnapshot(ctx context.Context, ref domain.WorkloadRef) (*domain.Snapshot, error)
	PowerDown(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error
	Restore(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error
}
