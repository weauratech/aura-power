package background

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	// This is intentionally idempotent and retried every cycle. A temporary API
	// failure during startup must not leave built-in schedules absent forever.
	if err := SeedBuiltInSchedules(ctx, d.Client, d.Config.Namespace); err != nil {
		log.Error(err, "failed to seed one or more built-in schedules")
	}

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

	// Process namespace annotations for implicit policies
	d.processNamespaceAnnotations(ctx)
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

	targetName := powerTargetName(wl.Ref)
	key := types.NamespacedName{Namespace: d.Config.Namespace, Name: targetName}

	// Check if PowerTarget already exists
	var existing v1alpha1.PowerTarget
	err := d.Client.Get(ctx, key, &existing)
	if err == nil {
		if existing.Spec.TargetRef.UID != wl.Ref.UID || existing.Spec.TargetRef.Kind != string(wl.Ref.Kind) {
			// A recreated object must never inherit the previous object's snapshot.
			existing.Status.Snapshot = nil
			if err := d.Client.Status().Update(ctx, &existing); err != nil {
				return false, fmt.Errorf("failed to clear stale target status: %w", err)
			}
			existing.Spec.TargetRef = targetReference(wl.Ref)
			if existing.Labels == nil {
				existing.Labels = map[string]string{}
			}
			existing.Labels["power.aura.sh/target-kind"] = string(wl.Ref.Kind)
			existing.Labels["power.aura.sh/target-uid"] = wl.Ref.UID
			if err := d.Client.Update(ctx, &existing); err != nil {
				return false, fmt.Errorf("failed to bind recreated target UID: %w", err)
			}
		}
		// Exists — update observed state
		return false, d.updateObservedState(ctx, &existing, wl)
	}
	if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to read PowerTarget: %w", err)
	}

	// Migrate the v2.1 target name without dropping a persisted snapshot.
	legacyKey := types.NamespacedName{Namespace: d.Config.Namespace, Name: fmt.Sprintf("%s--%s", wl.Ref.Namespace, wl.Ref.Name)}
	var legacy v1alpha1.PowerTarget
	legacyErr := d.Client.Get(ctx, legacyKey, &legacy)
	if legacyErr == nil && legacy.Spec.TargetRef.Kind == string(wl.Ref.Kind) {
		created, createErr := d.newPowerTarget(ctx, targetName, wl, &legacy.Status)
		if createErr != nil {
			return false, createErr
		}
		if err := d.Client.Delete(ctx, &legacy); err != nil {
			return created, fmt.Errorf("migrated target but failed to remove legacy identity: %w", err)
		}
		return created, nil
	}
	if legacyErr != nil && !apierrors.IsNotFound(legacyErr) {
		return false, fmt.Errorf("failed to read legacy PowerTarget: %w", legacyErr)
	}

	return d.newPowerTarget(ctx, targetName, wl, nil)
}

func (d *DiscoveryLoop) newPowerTarget(ctx context.Context, targetName string, wl ports.DiscoveredWorkload, previous *v1alpha1.PowerTargetStatus) (bool, error) {
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
				"power.aura.sh/target-uid":       wl.Ref.UID,
			},
		},
		Spec: v1alpha1.PowerTargetSpec{
			TargetRef: targetReference(wl.Ref),
		},
	}

	if err := d.Client.Create(ctx, target); err != nil {
		return false, fmt.Errorf("failed to create PowerTarget: %w", err)
	}

	// Update status (separate call since status is a subresource).
	// Preserve execution state during the identity migration, then refresh the
	// discovery projection from the current workload.
	if previous != nil {
		previous.DeepCopyInto(&target.Status)
	}
	target.Status.WorkloadLabels = copyStringMap(wl.Labels)
	target.Status.NamespaceLabels = copyStringMap(wl.NamespaceLabels)
	target.Status.ObservedState = v1alpha1.ObservedStateSpec{
		Replicas:   wl.Replicas,
		Suspended:  wl.Suspended,
		PowerState: powerState,
	}
	target.Status.Ownership = ownershipSpecs
	if err := d.Client.Status().Update(ctx, target); err != nil {
		return true, fmt.Errorf("created target but failed to update status: %w", err)
	}

	return true, nil
}

func powerTargetName(ref domain.WorkloadRef) string {
	identity := fmt.Sprintf("%s\x00%s\x00%s", ref.Namespace, ref.Kind, ref.Name)
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("target-%x", sum[:16])
}

func targetReference(ref domain.WorkloadRef) v1alpha1.TargetReference {
	return v1alpha1.TargetReference{Cluster: ref.Cluster, APIVersion: ref.APIVersion, Namespace: ref.Namespace, Name: ref.Name, Kind: string(ref.Kind), UID: ref.UID}
}

func (d *DiscoveryLoop) updateObservedState(ctx context.Context, target *v1alpha1.PowerTarget, wl ports.DiscoveredWorkload) error {
	previous := target.DeepCopy().Status
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
	target.Status.WorkloadLabels = copyStringMap(wl.Labels)
	target.Status.NamespaceLabels = copyStringMap(wl.NamespaceLabels)
	target.Status.Ownership = ownershipSpecs

	if reflect.DeepEqual(previous, target.Status) {
		return nil
	}
	return d.Client.Status().Update(ctx, target)
}

func copyStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
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
		expected[powerTargetName(wl.Ref)] = true
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

// processNamespaceAnnotations reads aura.sh/default-schedule annotations from namespaces
// and creates implicit low-priority policies for them.
func (d *DiscoveryLoop) processNamespaceAnnotations(ctx context.Context) {
	log := ctrl.Log.WithName("discovery.annotations")

	// List all namespaces
	var nsList corev1.NamespaceList
	if err := d.Client.List(ctx, &nsList); err != nil {
		log.Error(err, "failed to list namespaces for annotation processing")
		return
	}

	// Load existing PowerSchedules (named schedules)
	var schedules v1alpha1.PowerScheduleList
	if err := d.Client.List(ctx, &schedules); err != nil {
		log.Error(err, "failed to list power schedules")
		return
	}
	scheduleMap := map[string]*v1alpha1.PowerSchedule{}
	for i := range schedules.Items {
		scheduleMap[schedules.Items[i].Name] = &schedules.Items[i]
	}

	for _, ns := range nsList.Items {
		scheduleName := ns.Annotations["aura.sh/default-schedule"]
		if scheduleName == "" {
			continue
		}

		// Skip system namespaces
		isSystem := false
		for _, sysNs := range d.Config.SystemNamespaces {
			if ns.Name == sysNs {
				isSystem = true
				break
			}
		}
		if isSystem {
			continue
		}

		// Resolve named schedule
		schedule, exists := scheduleMap[scheduleName]
		if !exists {
			log.Info("namespace references unknown schedule", "namespace", ns.Name, "schedule", scheduleName)
			continue
		}

		// Determine priority from annotation (default: 0)
		priority := int32(0)
		if p := ns.Annotations["aura.sh/power-priority"]; p != "" {
			if parsed, err := strconv.Atoi(p); err == nil {
				priority = int32(parsed)
			}
		}

		// Create/update implicit policy
		policyName := fmt.Sprintf("ns-default-%s", ns.Name)
		key := types.NamespacedName{Namespace: d.Config.Namespace, Name: policyName}

		var existingPolicy v1alpha1.PowerPolicy
		err := d.Client.Get(ctx, key, &existingPolicy)
		if err == nil {
			// Policy exists — check if it needs updating
			if existingPolicy.Spec.Priority == priority &&
				existingPolicy.Spec.Schedule.DesiredState == schedule.Spec.DesiredState {
				continue // No change needed
			}
			// Update
			existingPolicy.Spec.Priority = priority
			existingPolicy.Spec.Schedule.DesiredState = schedule.Spec.DesiredState
			existingPolicy.Spec.Schedule.Windows = schedule.Spec.Windows
			if err := d.Client.Update(ctx, &existingPolicy); err != nil {
				log.Error(err, "failed to update implicit policy", "namespace", ns.Name)
			}
			continue
		}

		// Create new implicit policy
		policy := &v1alpha1.PowerPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      policyName,
				Namespace: d.Config.Namespace,
				Labels: map[string]string{
					"power.aura.sh/source":    "namespace-annotation",
					"power.aura.sh/namespace": ns.Name,
				},
			},
			Spec: v1alpha1.PowerPolicySpec{
				Scope: v1alpha1.PolicyScope{
					Namespaces: []string{ns.Name},
				},
				Schedule: v1alpha1.PolicySchedule{
					DesiredState: schedule.Spec.DesiredState,
					Windows:      schedule.Spec.Windows,
				},
				Priority:    priority,
				Description: fmt.Sprintf("Auto-generated from namespace %s annotation (schedule: %s)", ns.Name, scheduleName),
			},
		}

		if err := d.Client.Create(ctx, policy); err != nil {
			log.Error(err, "failed to create implicit policy", "namespace", ns.Name, "schedule", scheduleName)
		} else {
			log.Info("created implicit policy from namespace annotation", "namespace", ns.Name, "policy", policyName, "schedule", scheduleName)
		}
	}
}
