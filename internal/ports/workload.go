package ports

import (
	"context"

	"github.com/weauratech/aura-power/internal/core/domain"
)

// DiscoveredWorkload represents a workload found during discovery.
type DiscoveredWorkload struct {
	Ref                  domain.WorkloadRef
	Replicas             int32
	Suspended            bool
	Annotations          map[string]string
	Labels               map[string]string
	NamespaceLabels      map[string]string
	NamespaceAnnotations map[string]string
	Resources            domain.ResourceSummary
}

// WorkloadDiscoverer discovers Kubernetes workloads in the cluster.
type WorkloadDiscoverer interface {
	DiscoverAll(ctx context.Context, namespaces []string) ([]DiscoveredWorkload, error)
	DiscoverByNamespace(ctx context.Context, namespace string) ([]DiscoveredWorkload, error)
}

// WorkloadExecutor performs power actions on workloads.
type WorkloadExecutor interface {
	PowerDown(ctx context.Context, ref domain.WorkloadRef) (*domain.Snapshot, error)
	Restore(ctx context.Context, ref domain.WorkloadRef, snapshot domain.Snapshot) error
}
