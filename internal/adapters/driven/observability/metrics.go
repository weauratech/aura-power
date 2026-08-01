package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/weauratech/aura-power/internal/ports"
)

var (
	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "aura_power_reconciliation_duration_seconds",
			Help:    "Duration of reconciliation cycles",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"reconciler", "result"},
	)

	actionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aura_power_actions_total",
			Help: "Total power actions taken",
		},
		[]string{"action", "result"},
	)

	targetsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "aura_power_targets_total",
			Help: "Total number of targets by state",
		},
		[]string{"state"},
	)

	savingsCPUTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aura_power_savings_cpu_hours_total",
			Help: "Total CPU hours saved",
		},
		[]string{"namespace", "policy"},
	)

	savingsMemTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aura_power_savings_memory_gib_hours_total",
			Help: "Total memory GiB hours saved",
		},
		[]string{"namespace", "policy"},
	)

	savingsCostTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "aura_power_savings_estimated_cost_total",
			Help: "Total estimated cost saved",
		},
		[]string{"namespace", "policy"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		reconcileDuration,
		actionsTotal,
		targetsGauge,
		savingsCPUTotal,
		savingsMemTotal,
		savingsCostTotal,
	)
}

// PrometheusExporter implements the MetricsExporter port.
type PrometheusExporter struct{}

func NewPrometheusExporter() *PrometheusExporter {
	return &PrometheusExporter{}
}

func (p *PrometheusExporter) RecordReconciliation(duration time.Duration, err error) {
	result := "success"
	if err != nil {
		result = "error"
	}
	reconcileDuration.WithLabelValues("target", result).Observe(duration.Seconds())
}

func (p *PrometheusExporter) RecordAction(action ports.ActionType, target string, success bool) {
	result := "success"
	if !success {
		result = "error"
	}
	actionsTotal.WithLabelValues(string(action), result).Inc()
}

func (p *PrometheusExporter) SetGauge(metric ports.MetricName, value float64, labels map[string]string) {
	switch metric {
	case ports.MetricTargetsTotal, ports.MetricTargetsPoweredDown:
		state := labels["state"]
		targetsGauge.WithLabelValues(state).Set(value)
	}
}
