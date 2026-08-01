package ports

import (
	"context"
	"time"
)

// MetricsSample represents a single data point in a time series.
type MetricsSample struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// MetricsSeries represents a labeled time series.
type MetricsSeries struct {
	Labels  map[string]string `json:"labels"`
	Samples []MetricsSample   `json:"samples"`
}

// ClusterMetrics holds cluster-wide resource metrics.
type ClusterMetrics struct {
	CPUUsage        []MetricsSample `json:"cpuUsage"`
	CPUCapacity     []MetricsSample `json:"cpuCapacity"`
	CPURequested    []MetricsSample `json:"cpuRequested"`
	MemoryUsage     []MetricsSample `json:"memoryUsage"`
	MemoryCapacity  []MetricsSample `json:"memoryCapacity"`
	MemoryRequested []MetricsSample `json:"memoryRequested"`
	NodeCount       []MetricsSample `json:"nodeCount"`
}

// NamespaceMetrics holds metrics for a specific namespace.
type NamespaceMetrics struct {
	Namespace     string          `json:"namespace"`
	CPUUsage      []MetricsSample `json:"cpuUsage"`
	CPURequested  []MetricsSample `json:"cpuRequested"`
	MemoryUsage   []MetricsSample `json:"memoryUsage"`
	MemoryRequested []MetricsSample `json:"memoryRequested"`
}

// WorkloadMetrics holds metrics for a specific workload.
type WorkloadMetrics struct {
	Namespace     string          `json:"namespace"`
	Name          string          `json:"name"`
	CPUUsage      []MetricsSample `json:"cpuUsage"`
	CPURequested  []MetricsSample `json:"cpuRequested"`
	MemoryUsage   []MetricsSample `json:"memoryUsage"`
	MemoryRequested []MetricsSample `json:"memoryRequested"`
}

// NodeMetrics holds node count and cost data over time.
type NodeMetrics struct {
	NodeCount  []MetricsSample `json:"nodeCount"`
	TotalCost  []MetricsSample `json:"totalCost"`  // $/hour from OpenCost
	SavedCost  []MetricsSample `json:"savedCost"`  // $/hour saved by Aura Power
}

// CostSummary holds current cost information.
type CostSummary struct {
	TotalClusterCostPerHour   float64            `json:"totalClusterCostPerHour"`
	CostByNamespace           map[string]float64 `json:"costByNamespace"`
	SavedCostPerHour          float64            `json:"savedCostPerHour"`
	ProjectedMonthlySavings   float64            `json:"projectedMonthlySavings"`
}

// MetricsRange defines the time range for a metrics query.
type MetricsRange struct {
	Duration time.Duration // 1h, 6h, 24h, 7d
	Step     time.Duration // query resolution
}

// ParseRange converts a string range to MetricsRange.
func ParseRange(r string) MetricsRange {
	switch r {
	case "1h":
		return MetricsRange{Duration: time.Hour, Step: 15 * time.Second}
	case "6h":
		return MetricsRange{Duration: 6 * time.Hour, Step: time.Minute}
	case "24h":
		return MetricsRange{Duration: 24 * time.Hour, Step: 5 * time.Minute}
	case "7d":
		return MetricsRange{Duration: 7 * 24 * time.Hour, Step: 30 * time.Minute}
	default:
		return MetricsRange{Duration: 24 * time.Hour, Step: 5 * time.Minute}
	}
}

// MetricsProvider is the port interface for querying resource metrics.
type MetricsProvider interface {
	// GetClusterMetrics returns cluster-wide CPU, memory, and node metrics.
	GetClusterMetrics(ctx context.Context, r MetricsRange) (*ClusterMetrics, error)

	// GetNamespaceMetrics returns resource metrics for a specific namespace.
	GetNamespaceMetrics(ctx context.Context, namespace string, r MetricsRange) (*NamespaceMetrics, error)

	// GetWorkloadMetrics returns resource metrics for a specific workload.
	GetWorkloadMetrics(ctx context.Context, namespace, name string, r MetricsRange) (*WorkloadMetrics, error)

	// GetNodeMetrics returns node count and cost over time.
	GetNodeMetrics(ctx context.Context, r MetricsRange) (*NodeMetrics, error)

	// IsAvailable checks if the metrics provider is reachable.
	IsAvailable(ctx context.Context) bool

	// Name returns the provider name for logging.
	Name() string
}

// CostProvider is the port interface for cost data (OpenCost).
type CostProvider interface {
	// GetCostSummary returns current cost breakdown.
	GetCostSummary(ctx context.Context) (*CostSummary, error)

	// GetWorkloadCostPerHour returns the hourly cost of a specific workload.
	GetWorkloadCostPerHour(ctx context.Context, namespace, name string) (float64, error)

	// IsAvailable checks if OpenCost is reachable.
	IsAvailable(ctx context.Context) bool
}
