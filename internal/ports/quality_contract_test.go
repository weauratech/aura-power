package ports

import (
	"testing"
	"time"
)

func TestQualityMetricsRangeContract(t *testing.T) {
	for _, tc := range []struct {
		input          string
		duration, step time.Duration
	}{
		{"1h", time.Hour, 15 * time.Second}, {"6h", 6 * time.Hour, time.Minute},
		{"24h", 24 * time.Hour, 5 * time.Minute}, {"7d", 168 * time.Hour, 30 * time.Minute},
		{"", 24 * time.Hour, 5 * time.Minute}, {"invalid", 24 * time.Hour, 5 * time.Minute},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got := ParseRange(tc.input)
			if got.Duration != tc.duration || got.Step != tc.step {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
