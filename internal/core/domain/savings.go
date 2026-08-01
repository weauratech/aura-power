package domain

import "time"

// EstimateSavings computes savings for a target based on actual off-time minus divergence.
func EstimateSavings(target Target, offDuration, divergenceTime time.Duration, costConfig CostConfig) SavingsEstimate {
	effectiveOff := offDuration - divergenceTime
	if effectiveOff < 0 {
		effectiveOff = 0
	}

	var cpuHours, memGiBHours float64

	if target.Snapshot != nil {
		cpuHours = (float64(target.Snapshot.Resources.CPUMillicores) / 1000.0) * effectiveOff.Hours()
		memGiBHours = (float64(target.Snapshot.Resources.MemoryMiB) / 1024.0) * effectiveOff.Hours()
	}

	estimatedCost := (cpuHours * costConfig.CPUPerHour) + (memGiBHours * costConfig.MemoryPerGiB)

	return SavingsEstimate{
		Target:         target.Ref,
		CPUHoursSaved:  cpuHours,
		MemoryGiBHours: memGiBHours,
		EstimatedCost:  estimatedCost,
		OffDuration:    offDuration,
		DivergenceTime: divergenceTime,
	}
}

// EstimatePotentialSavings estimates savings potential WITHOUT a snapshot (for Discovery Mode).
// Uses current resource requests directly.
func EstimatePotentialSavings(ref WorkloadRef, resources ResourceSummary, offHoursPerMonth time.Duration, costConfig CostConfig) SavingsEstimate {
	cpuHours := (float64(resources.CPUMillicores) / 1000.0) * offHoursPerMonth.Hours()
	memGiBHours := (float64(resources.MemoryMiB) / 1024.0) * offHoursPerMonth.Hours()
	estimatedCost := (cpuHours * costConfig.CPUPerHour) + (memGiBHours * costConfig.MemoryPerGiB)

	return SavingsEstimate{
		Target:         ref,
		CPUHoursSaved:  cpuHours,
		MemoryGiBHours: memGiBHours,
		EstimatedCost:  estimatedCost,
		OffDuration:    offHoursPerMonth,
	}
}

// AggregateSavings aggregates savings across multiple targets.
func AggregateSavings(estimates []SavingsEstimate) SavingsSummary {
	summary := SavingsSummary{
		ByNamespace: make(map[string]SavingsEstimate),
		ByPolicy:    make(map[string]SavingsEstimate),
	}

	for _, e := range estimates {
		summary.TotalCPUHours += e.CPUHoursSaved
		summary.TotalMemoryGiB += e.MemoryGiBHours
		summary.TotalCost += e.EstimatedCost

		// Aggregate by namespace
		ns := e.Target.Namespace
		existing := summary.ByNamespace[ns]
		existing.CPUHoursSaved += e.CPUHoursSaved
		existing.MemoryGiBHours += e.MemoryGiBHours
		existing.EstimatedCost += e.EstimatedCost
		summary.ByNamespace[ns] = existing
	}

	return summary
}
