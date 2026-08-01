package background

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

// DiscoveryLoop periodically discovers workloads and creates/updates PowerTarget CRDs.
type DiscoveryLoop struct {
	Client     client.Client
	Discoverer ports.WorkloadDiscoverer
	Config     DiscoveryConfig
}

// DiscoveryConfig holds configuration for the discovery loop.
type DiscoveryConfig struct {
	Interval         time.Duration
	Namespace        string // Namespace where PowerTargets are created (e.g., aura-system)
	SystemNamespaces []string
	OptInAnnotation  string
	ExemptAnnotation string
}

// Run starts the discovery loop. Blocks until context is cancelled.
func (d *DiscoveryLoop) Run(ctx context.Context) {
	log := ctrl.Log.WithName("discovery")
	log.Info("starting discovery loop", "interval", d.Config.Interval)

	// Wait a bit for leader election to complete
	log.Info("waiting for leader election...")
	time.Sleep(5 * time.Second)

	// Run immediately after cache is ready
	d.runDiscovery(ctx)

	ticker := time.NewTicker(d.Config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("discovery loop stopped")
			return
		case <-ticker.C:
			d.runDiscovery(ctx)
		}
	}
}

// Start implements the controller-runtime Runnable interface.
// Called by the manager AFTER cache sync is complete.
func (d *DiscoveryLoop) Start(ctx context.Context) error {
	d.Run(ctx)
	return nil
}

func (d *DiscoveryLoop) runDiscovery(ctx context.Context) {
	log := ctrl.Log.WithName("discovery")
	start := time.Now()

	// Discover all workloads (nil = all namespaces, discoverer lists them)
	workloads, err := d.Discoverer.DiscoverAll(ctx, nil)
	if err != nil {
		log.Error(err, "discovery failed")
		return
	}

	// Create/update PowerTargets
	created, updated := 0, 0
	for _, wl := range workloads {
		wasCreated, err := d.ensurePowerTarget(ctx, wl)
		if err != nil {
			log.Error(err, "failed to ensure PowerTarget", "workload", wl.Ref.Namespace+"/"+wl.Ref.Name)
			continue
		}
		if wasCreated {
			created++
		} else {
			updated++
		}
	}

	// Cleanup orphaned PowerTargets
	orphaned := d.cleanupOrphans(ctx, workloads)

	duration := time.Since(start)
	log.Info("discovery cycle complete",
		"duration", duration,
		"workloads", len(workloads),
		"created", created,
		"updated", updated,
		"orphaned", orphaned,
	)
}

func (d *DiscoveryLoop) getEligibleNamespaces(ctx context.Context) []string {
	// Empty means "discover all namespaces" — the discoverer handles listing internally
	// and we filter system namespaces when creating targets
	return nil
}

func (d *DiscoveryLoop) ensurePowerTarget(ctx context.Context, wl ports.DiscoveredWorkload) (bool, error) {
	// Skip exempt workloads
	if wl.Annotations[d.Config.ExemptAnnotation] == "true" {
		return false, nil
	}

	// Skip system namespaces
	for _, sysNs := range d.Config.SystemNamespaces {
		if wl.Ref.Namespace == sysNs {
			return false, nil
		}
	}

	targetName := fmt.Sprintf("%s--%s", wl.Ref.Namespace, wl.Ref.Name)
	key := types.NamespacedName{Namespace: d.Config.Namespace, Name: targetName}

	// Check if PowerTarget already exists
	var existing v1alpha1.PowerTarget
	err := d.Client.Get(ctx, key, &existing)
	if err == nil {
		// Exists — update observed state
		return false, d.updateObservedState(ctx, &existing, wl)
	}

	// Detect ownership
	ownership := domain.DetectOwnership(wl.Annotations, wl.Labels, d.Config.OptInAnnotation, wl.NamespaceAnnotations)
	var ownershipSpecs []v1alpha1.OwnershipSpec
	for _, o := range ownership {
		ownershipSpecs = append(ownershipSpecs, v1alpha1.OwnershipSpec{
			Type:    string(o.Type),
			OptedIn: o.OptedIn,
		})
	}

	// Determine observed power state
	powerState := "on"
	if wl.Ref.Kind == domain.WorkloadKindCronJob && wl.Suspended {
		powerState = "off"
	} else if wl.Ref.Kind != domain.WorkloadKindCronJob && wl.Replicas == 0 {
		powerState = "off"
	}

	// Create new PowerTarget
	target := &v1alpha1.PowerTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetName,
			Namespace: d.Config.Namespace,
			Labels: map[string]string{
				"power.aura.sh/target-namespace": wl.Ref.Namespace,
				"power.aura.sh/target-name":      wl.Ref.Name,
				"power.aura.sh/target-kind":      string(wl.Ref.Kind),
			},
		},
		Spec: v1alpha1.PowerTargetSpec{
			TargetRef: v1alpha1.TargetReference{
				Namespace: wl.Ref.Namespace,
				Name:      wl.Ref.Name,
				Kind:      string(wl.Ref.Kind),
			},
		},
	}

	if err := d.Client.Create(ctx, target); err != nil {
		return false, fmt.Errorf("failed to create PowerTarget: %w", err)
	}

	// Update status (separate call since status is a subresource)
	target.Status = v1alpha1.PowerTargetStatus{
		ObservedState: v1alpha1.ObservedStateSpec{
			Replicas:   wl.Replicas,
			Suspended:  wl.Suspended,
			PowerState: powerState,
		},
		Ownership: ownershipSpecs,
	}
	if err := d.Client.Status().Update(ctx, target); err != nil {
		return true, fmt.Errorf("created target but failed to update status: %w", err)
	}

	return true, nil
}

func (d *DiscoveryLoop) updateObservedState(ctx context.Context, target *v1alpha1.PowerTarget, wl ports.DiscoveredWorkload) error {
	powerState := "on"
	if wl.Ref.Kind == domain.WorkloadKindCronJob && wl.Suspended {
		powerState = "off"
	} else if wl.Ref.Kind != domain.WorkloadKindCronJob && wl.Replicas == 0 {
		powerState = "off"
	}

	// Update ownership
	ownership := domain.DetectOwnership(wl.Annotations, wl.Labels, d.Config.OptInAnnotation, wl.NamespaceAnnotations)
	var ownershipSpecs []v1alpha1.OwnershipSpec
	for _, o := range ownership {
		ownershipSpecs = append(ownershipSpecs, v1alpha1.OwnershipSpec{
			Type:    string(o.Type),
			OptedIn: o.OptedIn,
		})
	}

	target.Status.ObservedState = v1alpha1.ObservedStateSpec{
		Replicas:   wl.Replicas,
		Suspended:  wl.Suspended,
		PowerState: powerState,
	}
	target.Status.Ownership = ownershipSpecs

	return d.Client.Status().Update(ctx, target)
}

func (d *DiscoveryLoop) cleanupOrphans(ctx context.Context, currentWorkloads []ports.DiscoveredWorkload) int {
	// Build a set of expected target names
	expected := make(map[string]bool)
	for _, wl := range currentWorkloads {
		// Skip system namespaces and exempt
		skip := false
		for _, sysNs := range d.Config.SystemNamespaces {
			if wl.Ref.Namespace == sysNs {
				skip = true
				break
			}
		}
		if skip || wl.Annotations[d.Config.ExemptAnnotation] == "true" {
			continue
		}
		expected[fmt.Sprintf("%s--%s", wl.Ref.Namespace, wl.Ref.Name)] = true
	}

	// List all PowerTargets
	var targets v1alpha1.PowerTargetList
	if err := d.Client.List(ctx, &targets, client.InNamespace(d.Config.Namespace)); err != nil {
		return 0
	}

	// Delete orphans
	deleted := 0
	for _, t := range targets.Items {
		if !expected[t.Name] {
			if err := d.Client.Delete(ctx, &t); err == nil {
				deleted++
			}
		}
	}

	return deleted
}
