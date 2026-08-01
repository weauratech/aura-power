package domain

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// PBT-08: Savings are always non-negative
func TestPropertySavingsNonNegative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cpuMillis := int64(rapid.IntRange(0, 16000).Draw(t, "cpu"))
		memMiB := int64(rapid.IntRange(0, 65536).Draw(t, "mem"))
		offHours := rapid.IntRange(0, 168).Draw(t, "offH")
		divHours := rapid.IntRange(0, 200).Draw(t, "divH")

		target := Target{
			Ref: WorkloadRef{Namespace: "test", Name: "app", Kind: WorkloadKindDeployment},
			Snapshot: &Snapshot{
				Resources: ResourceSummary{CPUMillicores: cpuMillis, MemoryMiB: memMiB},
			},
		}

		offDuration := time.Duration(offHours) * time.Hour
		divergence := time.Duration(divHours) * time.Hour

		result := EstimateSavings(target, offDuration, divergence, DefaultCostConfig())

		if result.CPUHoursSaved < 0 {
			t.Fatalf("negative CPU savings: %f", result.CPUHoursSaved)
		}
		if result.MemoryGiBHours < 0 {
			t.Fatalf("negative memory savings: %f", result.MemoryGiBHours)
		}
		if result.EstimatedCost < 0 {
			t.Fatalf("negative cost savings: %f", result.EstimatedCost)
		}
	})
}
