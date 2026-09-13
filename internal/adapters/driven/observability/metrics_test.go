package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestControllerMemoryMetricsExposeRuntimeState(t *testing.T) {
	for name, collector := range map[string]struct {
		value func() float64
	}{
		"heap allocation": {func() float64 { return testutil.ToFloat64(processHeapAlloc) }},
		"heap in use":     {func() float64 { return testutil.ToFloat64(processHeapInUse) }},
		"memory limit":    {func() float64 { return testutil.ToFloat64(processMemoryLimit) }},
	} {
		if got := collector.value(); got <= 0 {
			t.Fatalf("%s metric=%v, want positive runtime value", name, got)
		}
	}
}
