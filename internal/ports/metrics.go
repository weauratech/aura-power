package ports

import "time"

// MetricName identifies a gauge metric.
type MetricName string

const (
	MetricTargetsTotal      MetricName = "aura_power_targets_total"
	MetricTargetsPoweredDown MetricName = "aura_power_targets_powered_down"
	MetricPoliciesActive    MetricName = "aura_power_policies_active"
	MetricOverridesActive   MetricName = "aura_power_overrides_active"
	MetricSavingsCPUHours   MetricName = "aura_power_savings_cpu_hours_total"
	MetricSavingsMemoryGiB  MetricName = "aura_power_savings_memory_gib_hours_total"
	MetricSavingsCost       MetricName = "aura_power_savings_estimated_cost_total"
)

// ActionType identifies the kind of action for metrics recording.
type ActionType string

const (
	ActionPowerDown ActionType = "power_down"
	ActionRestore   ActionType = "restore"
	ActionBlock     ActionType = "block"
)

// MetricsExporter provides an interface for exporting Prometheus metrics.
type MetricsExporter interface {
	RecordReconciliation(duration time.Duration, err error)
	RecordAction(action ActionType, target string, success bool)
	SetGauge(metric MetricName, value float64, labels map[string]string)
}
