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
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
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
	Executor   ports.WorkloadExecutor
	Audit      ports.AuditRecorder
	Config     DiscoveryConfig
}

// DiscoveryConfig holds configuration for the discovery loop.
type DiscoveryConfig struct {
	Interval              time.Duration
	Namespace             string // Namespace where PowerTargets are created (e.g., aura-system)
	SystemNamespaces      []string
	OptInAnnotation       string
	ExemptAnnotation      string
	ArgoTrackingLabelKeys []string
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

// NeedLeaderElection prevents competing discovery and orphan-cleanup loops.
// This is also required during rolling upgrades, where an older binary must
// not delete UID-bound targets created by the candidate.
func (d *DiscoveryLoop) NeedLeaderElection() bool { return true }

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
	// Skip system namespaces
	for _, sysNs := range d.Config.SystemNamespaces {
		if wl.Ref.Namespace == sysNs {
			return false, nil
		}
	}

	targetName := powerTargetName(wl.Ref)
	key := types.NamespacedName{Namespace: d.Config.Namespace, Name: targetName}
	exempt := wl.Annotations[d.Config.ExemptAnnotation] == "true"

	// Check if PowerTarget already exists
	var existing v1alpha1.PowerTarget
	err := d.Client.Get(ctx, key, &existing)
	if err == nil {
		// A previous migration may have created the UID-bound object and then
		// failed while writing its status. Resume that partial migration before
		// treating the new object as authoritative, otherwise the legacy object
		// can retain the only usable recovery snapshot indefinitely.
		if existing.Status.ObservedState.PowerState == "" {
			legacyKey := types.NamespacedName{Namespace: d.Config.Namespace, Name: fmt.Sprintf("%s--%s", wl.Ref.Namespace, wl.Ref.Name)}
			var legacy v1alpha1.PowerTarget
			legacyErr := d.Client.Get(ctx, legacyKey, &legacy)
			if legacyErr == nil && legacy.Spec.TargetRef.Kind == string(wl.Ref.Kind) {
				var previous *v1alpha1.PowerTargetStatus
				if legacy.Spec.TargetRef.UID != "" && legacy.Spec.TargetRef.UID == wl.Ref.UID {
					previous = &legacy.Status
				} else if legacy.Spec.TargetRef.UID == "" && legacySnapshotProvesPoweredDown(&legacy, wl) {
					previous = &legacy.Status
				}
				if workloadIsPoweredDown(wl) && legacy.Status.Snapshot != nil && previous == nil {
					return false, fmt.Errorf("refusing to resume ambiguous legacy snapshot migration for %s/%s; restore a verified live state first", wl.Ref.Namespace, wl.Ref.Name)
				}
				if previous != nil {
					previous.DeepCopyInto(&existing.Status)
					// The copied legacy observation may equal the live projection, but it
					// has not been written to the replacement object yet. Force the
					// refresh path to issue the single durable status update.
					existing.Status.ObservedState.PowerState = ""
				}
				if err := d.updateObservedState(ctx, &existing, wl); err != nil {
					return false, fmt.Errorf("failed to resume legacy target status migration: %w", err)
				}
				if err := d.Client.Delete(ctx, &legacy); err != nil {
					return false, fmt.Errorf("resumed target migration but failed to remove legacy identity: %w", err)
				}
				if exempt {
					return false, d.restoreBeforeExemption(ctx, wl)
				}
				return false, nil
			}
			if legacyErr != nil && !apierrors.IsNotFound(legacyErr) {
				return false, fmt.Errorf("failed to read legacy PowerTarget while resuming migration: %w", legacyErr)
			}
		}
		if existing.Spec.TargetRef.UID != wl.Ref.UID || existing.Spec.TargetRef.Kind != string(wl.Ref.Kind) {
			// A recreated object is a different Kubernetes identity. It must not
			// inherit snapshots, completed actions, failures, savings, or decision
			// state from the deleted object.
			existing.Status = v1alpha1.PowerTargetStatus{}
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
		if exempt {
			return false, d.restoreBeforeExemption(ctx, wl)
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
		if exempt {
			return false, d.restoreLegacyBeforeExemption(ctx, wl, &legacy)
		}
		// Legacy releases did not persist a UID. A snapshot on a workload that is
		// still observably powered down is the only recovery record available
		// during upgrade, so bind it once to the currently observed UID. An
		// already-running workload does not need that ambiguous legacy snapshot.
		var previous *v1alpha1.PowerTargetStatus
		if legacy.Spec.TargetRef.UID != "" && legacy.Spec.TargetRef.UID == wl.Ref.UID {
			previous = &legacy.Status
		} else if legacy.Spec.TargetRef.UID == "" && legacySnapshotProvesPoweredDown(&legacy, wl) {
			previous = &legacy.Status
		}
		if workloadIsPoweredDown(wl) && legacy.Status.Snapshot != nil && previous == nil {
			return false, fmt.Errorf("refusing to migrate ambiguous legacy snapshot for %s/%s; restore a verified live state first", wl.Ref.Namespace, wl.Ref.Name)
		}
		created, createErr := d.newPowerTarget(ctx, targetName, wl, previous)
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

	if exempt {
		return false, nil
	}
	return d.newPowerTarget(ctx, targetName, wl, nil)
}

// restoreLegacyBeforeExemption keeps the v2.1 recovery record until discovery
// observes that the workload is running again. Creating an empty replacement
// first would expose a window in which another reconciliation can discard the
// only usable snapshot.
func (d *DiscoveryLoop) restoreLegacyBeforeExemption(ctx context.Context, wl ports.DiscoveredWorkload, legacy *v1alpha1.PowerTarget) error {
	if legacy.Spec.TargetRef.UID != "" && legacy.Spec.TargetRef.UID != wl.Ref.UID {
		return fmt.Errorf("refusing legacy exemption recovery for unverified workload identity %s/%s", wl.Ref.Namespace, wl.Ref.Name)
	}
	if !workloadIsPoweredDown(wl) {
		return d.Client.Delete(ctx, legacy)
	}
	if !legacySnapshotProvesPoweredDown(legacy, wl) {
		return fmt.Errorf("refusing exemption recovery without a verified legacy snapshot for %s/%s", wl.Ref.Namespace, wl.Ref.Name)
	}
	if d.Executor == nil {
		return fmt.Errorf("cannot restore exempt workload without executor")
	}
	snapshot := domain.Snapshot{ReplicaCount: legacy.Status.Snapshot.ReplicaCount, Suspended: legacy.Status.Snapshot.Suspended}
	if err := d.Executor.Restore(ctx, wl.Ref, snapshot); err != nil {
		return fmt.Errorf("restore legacy target before exemption: %w", err)
	}
	if d.Audit != nil {
		_ = d.Audit.Record(ctx, ports.AuditEvent{Timestamp: time.Now(), Action: ports.AuditWorkloadRestored, Actor: "system/exemption", Target: wl.Ref, Result: "success", Reason: "restored legacy snapshot before applying workload exemption"})
	}
	return nil
}

func legacySnapshotProvesPoweredDown(legacy *v1alpha1.PowerTarget, wl ports.DiscoveredWorkload) bool {
	snapshot := legacy.Status.Snapshot
	if snapshot == nil || !snapshot.Available || snapshot.CapturedAt == nil || legacy.Status.ObservedState.PowerState != "off" || !workloadIsPoweredDown(wl) {
		return false
	}
	if wl.Ref.Kind == domain.WorkloadKindCronJob {
		return snapshot.Suspended != nil
	}
	// v2.1.x could persist the post-mutation value. Zero is therefore
	// indistinguishable from a legitimately zero-scaled workload and must never
	// be trusted as a recovery value during migration.
	return snapshot.ReplicaCount != nil && *snapshot.ReplicaCount > 0
}

func workloadIsPoweredDown(wl ports.DiscoveredWorkload) bool {
	if wl.Ref.Kind == domain.WorkloadKindCronJob {
		return wl.Suspended
	}
	return wl.Replicas == 0
}

// restoreBeforeExemption guarantees that opting out cannot strand a workload
// at zero replicas/suspended after deleting the only persisted snapshot.
func (d *DiscoveryLoop) restoreBeforeExemption(ctx context.Context, wl ports.DiscoveredWorkload) error {
	key := types.NamespacedName{Namespace: d.Config.Namespace, Name: powerTargetName(wl.Ref)}
	var target v1alpha1.PowerTarget
	if err := d.Client.Get(ctx, key, &target); err != nil {
		return client.IgnoreNotFound(err)
	}
	if target.Spec.TargetRef.UID == "" || target.Spec.TargetRef.UID != wl.Ref.UID {
		return fmt.Errorf("refusing exemption recovery for unverified workload identity %s/%s", wl.Ref.Namespace, wl.Ref.Name)
	}
	if target.Status.Snapshot == nil || !target.Status.Snapshot.Available {
		return d.Client.Delete(ctx, &target)
	}
	isOff := (wl.Ref.Kind == domain.WorkloadKindCronJob && wl.Suspended) || (wl.Ref.Kind != domain.WorkloadKindCronJob && wl.Replicas == 0)
	if isOff {
		if d.Executor == nil {
			return fmt.Errorf("cannot restore exempt workload without executor")
		}
		snapshot := domain.Snapshot{ReplicaCount: target.Status.Snapshot.ReplicaCount, Suspended: target.Status.Snapshot.Suspended}
		if err := d.Executor.Restore(ctx, wl.Ref, snapshot); err != nil {
			return fmt.Errorf("restore before exemption: %w", err)
		}
		if d.Audit != nil {
			_ = d.Audit.Record(ctx, ports.AuditEvent{Timestamp: time.Now(), Action: ports.AuditWorkloadRestored, Actor: "system/exemption", Target: wl.Ref, Result: "success", Reason: "restored before applying workload exemption"})
		}
		return nil
	}
	// A later discovery observes the restored state before deleting the
	// snapshot-bearing target, making recovery independently verifiable.
	return d.Client.Delete(ctx, &target)
}

func (d *DiscoveryLoop) newPowerTarget(ctx context.Context, targetName string, wl ports.DiscoveredWorkload, previous *v1alpha1.PowerTargetStatus) (bool, error) {
	// Detect ownership
	ownership := domain.DetectOwnershipWithArgoTracking(wl.Annotations, wl.Labels, d.Config.OptInAnnotation, d.Config.ArgoTrackingLabelKeys, wl.NamespaceAnnotations)
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
	firstAttempt := true
	statusBackoff := wait.Backoff{Steps: 8, Duration: 100 * time.Millisecond, Factor: 2, Jitter: 0.1}
	if err := retry.OnError(statusBackoff, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsNotFound(err)
	}, func() error {
		current := target.DeepCopy()
		if !firstAttempt {
			if err := d.Client.Get(ctx, types.NamespacedName{Namespace: target.Namespace, Name: target.Name}, current); err != nil {
				return err
			}
		}
		firstAttempt = false
		if previous != nil {
			previous.DeepCopyInto(&current.Status)
		}
		current.Status.WorkloadLabels = copyStringMap(wl.Labels)
		current.Status.NamespaceLabels = copyStringMap(wl.NamespaceLabels)
		current.Status.ObservedState = v1alpha1.ObservedStateSpec{
			Replicas:   wl.Replicas,
			Suspended:  wl.Suspended,
			ActiveJobs: wl.ActiveJobs,
			PowerState: powerState,
		}
		current.Status.Ownership = ownershipSpecs
		return d.Client.Status().Update(ctx, current)
	}); err != nil {
		return true, fmt.Errorf("created target but failed to update status: %w", err)
	}

	return true, nil
}

func powerTargetName(ref domain.WorkloadRef) string {
	identity := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s", ref.Cluster, ref.APIVersion, ref.Namespace, ref.Kind, ref.Name, ref.UID)
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
	ownership := domain.DetectOwnershipWithArgoTracking(wl.Annotations, wl.Labels, d.Config.OptInAnnotation, d.Config.ArgoTrackingLabelKeys, wl.NamespaceAnnotations)
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
		ActiveJobs: wl.ActiveJobs,
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
		if skip {
			continue
		}
		if wl.Annotations[d.Config.ExemptAnnotation] == "true" {
			// Keep the target for one more observation while an asynchronous
			// restore converges; ensurePowerTarget deletes it once live state is on.
			isOff := (wl.Ref.Kind == domain.WorkloadKindCronJob && wl.Suspended) || (wl.Ref.Kind != domain.WorkloadKindCronJob && wl.Replicas == 0)
			if isOff {
				expected[powerTargetName(wl.Ref)] = true
				expected[fmt.Sprintf("%s--%s", wl.Ref.Namespace, wl.Ref.Name)] = true
			}
			continue
		}
		expected[powerTargetName(wl.Ref)] = true
		if workloadIsPoweredDown(wl) {
			// Preserve legacy recovery evidence until migration succeeds or an
			// operator establishes a verified running state.
			expected[fmt.Sprintf("%s--%s", wl.Ref.Namespace, wl.Ref.Name)] = true
		}
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
			if !isOwnedNamespacePolicy(&existingPolicy, ns.Name) {
				log.Error(errImplicitPolicyOwnershipConflict,
					"refusing to overwrite policy that is not owned by namespace annotation reconciliation",
					"namespace", ns.Name, "policy", policyName,
					"source", existingPolicy.Labels["power.aura.sh/source"],
					"ownerNamespace", existingPolicy.Labels["power.aura.sh/namespace"])
				continue
			}
			desiredSpec := namespacePolicySpec(ns.Name, scheduleName, schedule, priority)
			// Policy exists and is controller-owned — reconcile its complete
			// generated contract, including scope and changed schedule windows.
			if reflect.DeepEqual(existingPolicy.Spec, desiredSpec) {
				continue // No change needed
			}
			existingPolicy.Spec = desiredSpec
			if err := d.Client.Update(ctx, &existingPolicy); err != nil {
				log.Error(err, "failed to update implicit policy", "namespace", ns.Name)
			}
			continue
		}
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to read implicit policy", "namespace", ns.Name, "policy", policyName)
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
			Spec: namespacePolicySpec(ns.Name, scheduleName, schedule, priority),
		}

		if err := d.Client.Create(ctx, policy); err != nil {
			log.Error(err, "failed to create implicit policy", "namespace", ns.Name, "schedule", scheduleName)
		} else {
			log.Info("created implicit policy from namespace annotation", "namespace", ns.Name, "policy", policyName, "schedule", scheduleName)
		}
	}
}

var errImplicitPolicyOwnershipConflict = fmt.Errorf("implicit policy name collides with a policy not owned by namespace annotation reconciliation")

func isOwnedNamespacePolicy(policy *v1alpha1.PowerPolicy, namespace string) bool {
	return policy.Labels["power.aura.sh/source"] == "namespace-annotation" &&
		policy.Labels["power.aura.sh/namespace"] == namespace
}

func namespacePolicySpec(namespace, scheduleName string, schedule *v1alpha1.PowerSchedule, priority int32) v1alpha1.PowerPolicySpec {
	return v1alpha1.PowerPolicySpec{
		Scope: v1alpha1.PolicyScope{Namespaces: []string{namespace}},
		Schedule: v1alpha1.PolicySchedule{
			DesiredState: schedule.Spec.DesiredState,
			Windows:      schedule.Spec.Windows,
		},
		Priority:    priority,
		Description: fmt.Sprintf("Auto-generated from namespace %s annotation (schedule: %s)", namespace, scheduleName),
	}
}
