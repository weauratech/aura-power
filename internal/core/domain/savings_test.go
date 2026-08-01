package domain

import (
	"testing"
	"time"
)

func TestEstimateSavings_Basic(t *testing.T) {
	target := Target{
		Ref: WorkloadRef{Namespace: "dev", Name: "api", Kind: WorkloadKindDeployment},
		Snapshot: &Snapshot{
			Resources: ResourceSummary{
				CPUMillicores: 2000, // 2 CPU
				MemoryMiB:     4096, // 4 GiB
			},
		},
	}

	offDuration := 10 * time.Hour
	divergence := time.Duration(0)
	config := DefaultCostConfig()

	result := EstimateSavings(target, offDuration, divergence, config)

	// CPU: 2 CPU * 10h = 20 CPU-hours
	if result.CPUHoursSaved != 20.0 {
		t.Fatalf("expected 20 CPU-hours, got %f", result.CPUHoursSaved)
	}

	// Memory: 4 GiB * 10h = 40 GiB-hours
	expectedMem := (4096.0 / 1024.0) * 10.0 // 4 GiB * 10h = 40
	if result.MemoryGiBHours != expectedMem {
		t.Fatalf("expected %f GiB-hours, got %f", expectedMem, result.MemoryGiBHours)
	}

	// Cost: (20 * 0.04) + (40 * 0.008) = 0.80 + 0.32 = 1.12
	expectedCost := (20.0 * 0.04) + (expectedMem * 0.008)
	if result.EstimatedCost != expectedCost {
		t.Fatalf("expected cost %f, got %f", expectedCost, result.EstimatedCost)
	}
}

func TestEstimateSavings_WithDivergence(t *testing.T) {
	target := Target{
		Ref: WorkloadRef{Namespace: "dev", Name: "api", Kind: WorkloadKindDeployment},
		Snapshot: &Snapshot{
			Resources: ResourceSummary{CPUMillicores: 1000, MemoryMiB: 1024},
		},
	}

	offDuration := 10 * time.Hour
	divergence := 3 * time.Hour // Was scaled up for 3h
	config := DefaultCostConfig()

	result := EstimateSavings(target, offDuration, divergence, config)

	// Effective off = 10 - 3 = 7 hours
	// CPU: 1 CPU * 7h = 7
	if result.CPUHoursSaved != 7.0 {
		t.Fatalf("expected 7 CPU-hours (10-3 divergence), got %f", result.CPUHoursSaved)
	}
}

func TestEstimateSavings_DivergenceExceedsOff(t *testing.T) {
	target := Target{
		Ref: WorkloadRef{Namespace: "dev", Name: "api", Kind: WorkloadKindDeployment},
		Snapshot: &Snapshot{
			Resources: ResourceSummary{CPUMillicores: 1000, MemoryMiB: 1024},
		},
	}

	offDuration := 5 * time.Hour
	divergence := 8 * time.Hour // More divergence than off-time
	config := DefaultCostConfig()

	result := EstimateSavings(target, offDuration, divergence, config)

	if result.CPUHoursSaved != 0 {
		t.Fatalf("expected 0 savings when divergence exceeds off-time, got %f", result.CPUHoursSaved)
	}
	if result.EstimatedCost != 0 {
		t.Fatalf("expected 0 cost, got %f", result.EstimatedCost)
	}
}

func TestEstimateSavings_NilSnapshot(t *testing.T) {
	target := Target{
		Ref:      WorkloadRef{Namespace: "dev", Name: "api", Kind: WorkloadKindDeployment},
		Snapshot: nil,
	}

	result := EstimateSavings(target, 10*time.Hour, 0, DefaultCostConfig())

	if result.CPUHoursSaved != 0 || result.EstimatedCost != 0 {
		t.Fatal("expected zero savings with nil snapshot")
	}
}

func TestAggregateSavings_MultipleTargets(t *testing.T) {
	estimates := []SavingsEstimate{
		{Target: WorkloadRef{Namespace: "dev", Name: "a"}, CPUHoursSaved: 10, MemoryGiBHours: 5, EstimatedCost: 0.50},
		{Target: WorkloadRef{Namespace: "dev", Name: "b"}, CPUHoursSaved: 20, MemoryGiBHours: 10, EstimatedCost: 1.00},
		{Target: WorkloadRef{Namespace: "staging", Name: "c"}, CPUHoursSaved: 5, MemoryGiBHours: 2, EstimatedCost: 0.25},
	}

	summary := AggregateSavings(estimates)

	if summary.TotalCPUHours != 35 {
		t.Fatalf("expected 35 total CPU hours, got %f", summary.TotalCPUHours)
	}
	if summary.TotalCost != 1.75 {
		t.Fatalf("expected 1.75 total cost, got %f", summary.TotalCost)
	}
	if summary.ByNamespace["dev"].CPUHoursSaved != 30 {
		t.Fatalf("expected 30 dev CPU hours, got %f", summary.ByNamespace["dev"].CPUHoursSaved)
	}
	if summary.ByNamespace["staging"].CPUHoursSaved != 5 {
		t.Fatalf("expected 5 staging CPU hours, got %f", summary.ByNamespace["staging"].CPUHoursSaved)
	}
}
