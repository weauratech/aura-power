package reconciler

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/adapters/selection"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

const defaultRequeueAfter = 30 * time.Second
const errorRequeueAfter = 10 * time.Second
const retryActionAnnotation = "power.aura.sh/retry-action"
const statusCheckpointInterval = 5 * time.Minute
const actionConvergenceGrace = 2 * time.Minute

// TargetReconciler reconciles PowerTarget objects.
type TargetReconciler struct {
	client.Client
	Config       domain.GuardrailConfig
	Executor     ports.WorkloadExecutor
	Audit        ports.AuditRecorder
	Metrics      ports.MetricsExporter
	RequeueAfter time.Duration
}

func (r *TargetReconciler) requeueAfter() time.Duration {
	if r.RequeueAfter > 0 {
		return r.RequeueAfter
	}
	return defaultRequeueAfter
}

func (r *TargetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	start := time.Now()
	logger := log.FromContext(ctx).WithValues("target", req.NamespacedName)

	// 1. Get PowerTarget
	var target v1alpha1.PowerTarget
	if err := r.Get(ctx, req.NamespacedName, &target); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Load policies and overrides
	policies, err := r.loadPolicies(ctx)
	if err != nil {
		logger.Error(err, "failed to load policies")
		return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
	}

	overrides, err := r.loadOverrides(ctx)
	if err != nil {
		logger.Error(err, "failed to load overrides")
		return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
	}

	// 3. Convert CRD target to domain target
	domainTarget := toDomainTarget(&target)
	if domainTarget.Ref.UID == "" {
		logger.Info("waiting for discovery to bind workload UID before mutation")
		return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
	}

	// 4. Compute decision
	now := time.Now()
	decision := domain.ComputeDecision(domainTarget, policies, overrides, r.Config, now)

	// 5. Update status. Keep a copy so stable reconciliations do not issue a
	// status write and trigger another watch event.
	previousStatus := target.DeepCopy().Status
	previousLabels := target.DeepCopy().Labels
	updateTargetStatus(&target, decision, now)
	if !equality.Semantic.DeepEqual(previousLabels, target.Labels) {
		if err := r.Update(ctx, &target); err != nil {
			logger.Error(err, "failed to persist target state label")
			return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
		}
	}

	// 6. Execute action if needed
	if !decision.IsBlocked() && decision.IsManaged() {
		observedState := domain.PowerStateFromObserved(domainTarget.ObservedState, domainTarget.Ref.Kind)
		handled, err := r.reconcileExistingAction(ctx, &target, decision, observedState)
		if err != nil {
			logger.Error(err, "failed to persist action observation")
			return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
		}
		if handled {
			return ctrl.Result{RequeueAfter: r.requeueAfter()}, nil
		}

		if decision.DesiredState == domain.PowerStateOff && observedState == domain.PowerStateOn {
			if target.Status.Snapshot == nil || !target.Status.Snapshot.Available {
				if err := r.captureAndPersistSnapshot(ctx, &target, domainTarget.Ref); err != nil {
					logger.Error(err, "failed to capture and persist snapshot before power-down")
					target.Status.ConsecutiveFailures++
					r.Metrics.RecordAction(ports.ActionPowerDown, req.String(), false)
					r.recordAudit(ctx, domainTarget.Ref, ports.AuditExecutionError, "error", err.Error(), "")
					return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
				}
			}
			if err := r.beginAction(ctx, &target, decision); err != nil {
				logger.Error(err, "failed to persist power-down intent")
				return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
			}
			if err := r.executePowerDown(ctx, &target, domainTarget.Ref); err != nil {
				logger.Error(err, "power-down failed")
				target.Status.ConsecutiveFailures++
				r.failAction(&target, err)
				r.Metrics.RecordAction(ports.ActionPowerDown, req.String(), false)
				r.recordAudit(ctx, domainTarget.Ref, ports.AuditExecutionError, "error", err.Error(), "")
				r.Status().Update(ctx, &target)
				return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
			}
			target.Status.ConsecutiveFailures = 0
			r.completeAction(&target, "Applied", "workload mutation accepted; waiting for observation")
			r.Metrics.RecordAction(ports.ActionPowerDown, req.String(), true)
			r.recordAudit(ctx, domainTarget.Ref, ports.AuditWorkloadPoweredDown, "success", "Powered down by policy", ruleNameFromDecision(decision))
			// Requeue faster to confirm pods terminated
			updateTargetStatus(&target, decision, time.Now())
			if err := r.Status().Update(ctx, &target); err != nil {
				logger.Error(err, "failed to update target status after power-down")
			}
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		if decision.DesiredState == domain.PowerStateOn && observedState == domain.PowerStateOff {
			if err := r.beginAction(ctx, &target, decision); err != nil {
				logger.Error(err, "failed to persist restore intent")
				return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
			}
			if err := r.executeRestore(ctx, &target, domainTarget.Ref); err != nil {
				logger.Error(err, "restore failed")
				target.Status.ConsecutiveFailures++
				r.failAction(&target, err)
				r.Metrics.RecordAction(ports.ActionRestore, req.String(), false)
				r.recordAudit(ctx, domainTarget.Ref, ports.AuditExecutionError, "error", err.Error(), "")
				r.Status().Update(ctx, &target)
				return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
			}
			target.Status.ConsecutiveFailures = 0
			r.completeAction(&target, "Applied", "workload mutation accepted; waiting for observation")
			r.Metrics.RecordAction(ports.ActionRestore, req.String(), true)
			r.recordAudit(ctx, domainTarget.Ref, ports.AuditWorkloadRestored, "success", "Restored from snapshot", ruleNameFromDecision(decision))
			// Requeue faster to confirm pods started
			updateTargetStatus(&target, decision, time.Now())
			if err := r.Status().Update(ctx, &target); err != nil {
				logger.Error(err, "failed to update target status after restore")
			}
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	// 7. Persist status update
	if !equality.Semantic.DeepEqual(previousStatus, target.Status) {
		if err := r.Status().Update(ctx, &target); err != nil {
			logger.Error(err, "failed to update target status")
			return ctrl.Result{RequeueAfter: errorRequeueAfter}, nil
		}
	}

	duration := time.Since(start)
	r.Metrics.RecordReconciliation(duration, nil)
	logger.Info("reconciliation complete", "duration", duration, "desiredState", decision.DesiredState)

	return ctrl.Result{RequeueAfter: r.requeueAfter()}, nil
}

// reconcileExistingAction makes a desired-state transition monotonic. Once a
// mutation has been accepted (or its result is uncertain after a controller
// crash), the controller observes convergence instead of issuing the same
// write again. A later divergence is reported as contention. This prevents
// write loops with Argo CD self-heal and other field managers.
func (r *TargetReconciler) reconcileExistingAction(ctx context.Context, target *v1alpha1.PowerTarget, decision domain.Decision, observed domain.PowerState) (bool, error) {
	desired := decision.DesiredState
	decisionKey := powerDecisionKey(decision)
	action := target.Status.Action
	if desired == domain.PowerStateOn && observed == desired && target.Status.Snapshot != nil &&
		(action == nil || action.DesiredState != string(domain.PowerStateOff) || action.Phase == "Converged") {
		// GitOps or an operator may already have restored the workload. Do not
		// replay the stale replica snapshot over that legitimate live state.
		target.Status.Snapshot = nil
		now := metav1.Now()
		target.Status.Action = &v1alpha1.PowerActionStatus{
			DesiredState: string(desired),
			DecisionKey:  decisionKey,
			Phase:        "Converged",
			CompletedAt:  &now,
			Message:      "desired workload state already observed",
			RetryToken:   target.Annotations[retryActionAnnotation],
		}
		action = target.Status.Action
	}
	if action == nil || action.DesiredState != string(desired) || action.DecisionKey != decisionKey || action.Phase == "Failed" {
		return false, nil
	}
	if target.Annotations[retryActionAnnotation] != action.RetryToken {
		return false, nil
	}

	if observed == desired {
		if action.Phase != "Converged" {
			r.completeAction(target, "Converged", "desired workload state observed")
		}
		return false, nil
	}
	if action.Phase == "InProgress" {
		// The controller may have stopped after persisting intent. Reissuing the
		// same UID-bound scale/suspend operation is idempotent and recovers both
		// the before-mutation and after-mutation crash windows.
		return false, nil
	}
	if action.Phase == "Applied" && action.CompletedAt != nil && time.Since(action.CompletedAt.Time) < actionConvergenceGrace {
		// Discovery is periodic, so the first reconciliation after a successful
		// write can still carry the pre-mutation observation. Give the API and
		// discovery loop time to converge before declaring another field owner.
		return true, nil
	}

	if action.Phase != "Contended" {
		action.Phase = "Contended"
		action.Message = "desired state was not retained; another controller may own the same field"
		action.CompletedAt = nil
		if err := r.Status().Update(ctx, target); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (r *TargetReconciler) beginAction(ctx context.Context, target *v1alpha1.PowerTarget, decision domain.Decision) error {
	now := metav1.Now()
	target.Status.Action = &v1alpha1.PowerActionStatus{
		DesiredState: string(decision.DesiredState),
		DecisionKey:  powerDecisionKey(decision),
		Phase:        "InProgress",
		AttemptedAt:  &now,
		RetryToken:   target.Annotations[retryActionAnnotation],
	}
	return r.Status().Update(ctx, target)
}

func powerDecisionKey(decision domain.Decision) string {
	if decision.WinningRule == nil {
		return string(decision.DesiredState)
	}
	rule := decision.WinningRule
	return fmt.Sprintf("%s/%s/%s@%d:%s", rule.Kind, rule.Namespace, rule.Name, rule.CreatedAt.UnixNano(), decision.DesiredState)
}

func (r *TargetReconciler) completeAction(target *v1alpha1.PowerTarget, phase, message string) {
	if target.Status.Action == nil {
		return
	}
	now := metav1.Now()
	target.Status.Action.Phase = phase
	target.Status.Action.CompletedAt = &now
	target.Status.Action.Message = message
}

func (r *TargetReconciler) failAction(target *v1alpha1.PowerTarget, err error) {
	if target.Status.Action == nil {
		return
	}
	target.Status.Action.Phase = "Failed"
	target.Status.Action.Message = err.Error()
	target.Status.Action.CompletedAt = nil
}

func (r *TargetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PowerTarget{}).
		Complete(r)
}

func (r *TargetReconciler) executePowerDown(ctx context.Context, target *v1alpha1.PowerTarget, ref domain.WorkloadRef) error {
	return r.Executor.PowerDown(ctx, ref)
}

func (r *TargetReconciler) captureAndPersistSnapshot(ctx context.Context, target *v1alpha1.PowerTarget, ref domain.WorkloadRef) error {
	snapshot, err := r.Executor.CaptureSnapshot(ctx, ref)
	if err != nil {
		return err
	}
	// Store snapshot on target status
	now := metav1.Now()
	target.Status.Snapshot = &v1alpha1.SnapshotSpec{
		Available:    true,
		ReplicaCount: snapshot.ReplicaCount,
		Suspended:    snapshot.Suspended,
		CapturedAt:   &now,
		Resources: v1alpha1.ResourceSpec{
			CPUMillicores: snapshot.Resources.CPUMillicores,
			MemoryMiB:     snapshot.Resources.MemoryMiB,
		},
	}
	return r.Status().Update(ctx, target)
}

func (r *TargetReconciler) executeRestore(ctx context.Context, target *v1alpha1.PowerTarget, ref domain.WorkloadRef) error {
	if target.Status.Snapshot == nil || !target.Status.Snapshot.Available {
		return nil // Snapshot missing — will be caught by decision engine (BlockSnapshotMissing)
	}
	snapshot := domain.Snapshot{
		ReplicaCount: target.Status.Snapshot.ReplicaCount,
		Suspended:    target.Status.Snapshot.Suspended,
	}
	if err := r.Executor.Restore(ctx, ref, snapshot); err != nil {
		return err
	}
	// Clear snapshot after successful restore
	target.Status.Snapshot = nil
	return nil
}

func (r *TargetReconciler) loadPolicies(ctx context.Context) ([]domain.PolicySpec, error) {
	var list v1alpha1.PowerPolicyList
	if err := r.List(ctx, &list); err != nil {
		return nil, err
	}
	policies := make([]domain.PolicySpec, 0, len(list.Items))
	for _, p := range list.Items {
		converted := toDomainPolicy(&p)
		resolved, err := selection.ResolveScope(ctx, r.Client, p.Namespace, p.Spec.Scope)
		if err != nil {
			// One malformed rule must fail closed for its own scope without
			// preventing unrelated targets from restoring or reconciling.
			log.FromContext(ctx).Error(err, "skipping policy with unresolved scope", "policy", client.ObjectKeyFromObject(&p))
			continue
		}
		converted.Scope = resolved
		policies = append(policies, converted)
	}
	return policies, nil
}

func (r *TargetReconciler) loadOverrides(ctx context.Context) ([]domain.OverrideSpec, error) {
	var list v1alpha1.PowerOverrideList
	if err := r.List(ctx, &list); err != nil {
		return nil, err
	}
	overrides := make([]domain.OverrideSpec, 0, len(list.Items))
	for _, o := range list.Items {
		converted := toDomainOverride(&o)
		resolved, err := selection.ResolveScope(ctx, r.Client, o.Namespace, o.Spec.Scope)
		if err != nil {
			log.FromContext(ctx).Error(err, "skipping override with unresolved scope", "override", client.ObjectKeyFromObject(&o))
			continue
		}
		converted.Scope = resolved
		overrides = append(overrides, converted)
	}
	return overrides, nil
}

func (r *TargetReconciler) recordAudit(ctx context.Context, ref domain.WorkloadRef, action ports.AuditAction, result, reason, ruleName string) {
	_ = r.Audit.Record(ctx, ports.AuditEvent{
		Timestamp: time.Now(),
		Action:    action,
		Actor:     "system/controller",
		Target:    ref,
		Result:    result,
		Reason:    reason,
		RuleName:  ruleName,
	})
}

func ruleNameFromDecision(d domain.Decision) string {
	if d.WinningRule != nil {
		return d.WinningRule.Name
	}
	return ""
}

func toDomainTarget(t *v1alpha1.PowerTarget) domain.Target {
	ref := domain.WorkloadRef{
		Cluster:    t.Spec.TargetRef.Cluster,
		APIVersion: t.Spec.TargetRef.APIVersion,
		Namespace:  t.Spec.TargetRef.Namespace,
		Name:       t.Spec.TargetRef.Name,
		Kind:       domain.WorkloadKind(t.Spec.TargetRef.Kind),
		UID:        t.Spec.TargetRef.UID,
	}

	observed := domain.ObservedState{
		Replicas:  t.Status.ObservedState.Replicas,
		Suspended: t.Status.ObservedState.Suspended,
	}

	var snapshot *domain.Snapshot
	if t.Status.Snapshot != nil && t.Status.Snapshot.Available {
		snapshot = &domain.Snapshot{
			ReplicaCount: t.Status.Snapshot.ReplicaCount,
			Suspended:    t.Status.Snapshot.Suspended,
			Resources: domain.ResourceSummary{
				CPUMillicores: t.Status.Snapshot.Resources.CPUMillicores,
				MemoryMiB:     t.Status.Snapshot.Resources.MemoryMiB,
			},
		}
	}

	var ownership []domain.OwnershipSignal
	for _, o := range t.Status.Ownership {
		ownership = append(ownership, domain.OwnershipSignal{
			Type:    domain.OwnershipType(o.Type),
			OptedIn: o.OptedIn,
		})
	}

	return domain.Target{
		Ref:             ref,
		ObservedState:   observed,
		Ownership:       ownership,
		Annotations:     t.GetAnnotations(),
		Labels:          t.Status.WorkloadLabels,
		NamespaceLabels: t.Status.NamespaceLabels,
		Snapshot:        snapshot,
	}
}

func updateTargetStatus(t *v1alpha1.PowerTarget, d domain.Decision, nowTime time.Time) {
	// Detect state transition — set LastTransition only when state changes
	previousDesired := t.Status.DesiredState
	newDesired := string(d.DesiredState)
	if previousDesired != newDesired && newDesired != "" {
		now := metav1.NewTime(nowTime)
		t.Status.LastTransition = &now
	}
	// Backfill: if lastTransition is nil but state is determined, set it now
	if t.Status.LastTransition == nil && newDesired != "" {
		now := metav1.NewTime(nowTime)
		t.Status.LastTransition = &now
	}

	t.Status.DesiredState = newDesired
	t.Status.Managed = d.IsManaged()
	t.Status.Divergent = d.Divergent
	t.Status.Blocked = d.IsBlocked()

	// Checkpoint cumulative savings at a bounded cadence. Updating this field on
	// every 30-second reconcile caused a write/watch feedback loop for every
	// target even when the decision and observed state were stable.
	checkpoint := t.Status.LastReconciliation == nil ||
		nowTime.Sub(t.Status.LastReconciliation.Time) >= statusCheckpointInterval ||
		previousDesired != newDesired
	if checkpoint {
		now := metav1.NewTime(nowTime)

		// Accumulate savings when target is powered off
		if newDesired == "off" && t.Status.ObservedState.PowerState == "off" && t.Status.LastReconciliation != nil {
			elapsed := now.Time.Sub(t.Status.LastReconciliation.Time)
			hours := elapsed.Hours()
			if hours > 0 && hours < 1 { // sanity: only accumulate within reasonable interval
				cpuCores := float64(0)
				memGiB := float64(0)
				if t.Status.Snapshot != nil && t.Status.Snapshot.Resources.CPUMillicores > 0 {
					cpuCores = float64(t.Status.Snapshot.Resources.CPUMillicores) / 1000.0
					memGiB = float64(t.Status.Snapshot.Resources.MemoryMiB) / 1024.0
				} else {
					// Default estimate if no resources captured
					cpuCores = 0.25
					memGiB = 0.5
				}
				if t.Status.Savings == nil {
					t.Status.Savings = &v1alpha1.SavingsSpec{}
				}
				t.Status.Savings.CPUHoursSaved += cpuCores * hours
				t.Status.Savings.MemoryGiBHours += memGiB * hours
				t.Status.Savings.EstimatedCost += (cpuCores*0.032 + memGiB*0.004) * hours
			}
		}

		t.Status.LastReconciliation = &now
	}

	if d.WinningRule != nil {
		t.Status.WinningRule = &v1alpha1.RuleReference{
			Kind:        string(d.WinningRule.Kind),
			Name:        d.WinningRule.Name,
			Namespace:   d.WinningRule.Namespace,
			Priority:    int32(d.WinningRule.Priority),
			Description: d.WinningRule.Description,
		}
	} else {
		t.Status.WinningRule = nil
	}

	t.Status.SuppressedRules = nil
	for _, sr := range d.SuppressedRules {
		t.Status.SuppressedRules = append(t.Status.SuppressedRules, v1alpha1.RuleReference{
			Kind:      string(sr.Kind),
			Name:      sr.Name,
			Namespace: sr.Namespace,
			Priority:  int32(sr.Priority),
		})
	}

	t.Status.BlockReasons = nil
	for _, br := range d.BlockReasons {
		t.Status.BlockReasons = append(t.Status.BlockReasons, v1alpha1.BlockReasonSpec{
			Type:     string(br.Type),
			Message:  br.Message,
			Waivable: br.Waivable,
		})
	}

	// Update power state label
	if t.Labels == nil {
		t.Labels = make(map[string]string)
	}
	if d.IsBlocked() {
		t.Labels["power.aura.sh/state"] = "blocked"
	} else if d.Divergent {
		t.Labels["power.aura.sh/state"] = "divergent"
	} else {
		t.Labels["power.aura.sh/state"] = string(d.DesiredState)
	}
}

func toDomainPolicy(p *v1alpha1.PowerPolicy) domain.PolicySpec {
	var windows []domain.TimeWindow
	for _, w := range p.Spec.Schedule.Windows {
		windows = append(windows, toDomainTimeWindow(w))
	}

	return domain.PolicySpec{
		Name:      p.Name,
		Namespace: p.Namespace,
		Scope: domain.Scope{
			TargetRefs:      toDomainTargetReferences(p.Spec.Scope.TargetRefs),
			Namespaces:      p.Spec.Scope.Namespaces,
			NamespaceGroups: p.Spec.Scope.NamespaceGroups,
			NamespaceLabels: p.Spec.Scope.NamespaceLabels,
			WorkloadNames:   p.Spec.Scope.WorkloadNames,
			WorkloadLabels:  p.Spec.Scope.WorkloadLabels,
		},
		Schedule: domain.Schedule{
			Windows:      windows,
			DesiredState: domain.PowerState(p.Spec.Schedule.DesiredState),
		},
		Priority:    domain.Priority(p.Spec.Priority),
		Description: p.Spec.Description,
		CreatedAt:   p.CreationTimestamp.Time,
	}
}

func toDomainOverride(o *v1alpha1.PowerOverride) domain.OverrideSpec {
	return domain.OverrideSpec{
		Name:      o.Name,
		Namespace: o.Namespace,
		Scope: domain.Scope{
			TargetRefs:      toDomainTargetReferences(o.Spec.Scope.TargetRefs),
			Namespaces:      o.Spec.Scope.Namespaces,
			NamespaceGroups: o.Spec.Scope.NamespaceGroups,
			NamespaceLabels: o.Spec.Scope.NamespaceLabels,
			WorkloadNames:   o.Spec.Scope.WorkloadNames,
			WorkloadLabels:  o.Spec.Scope.WorkloadLabels,
		},
		State:     domain.PowerState(o.Spec.State),
		Priority:  domain.Priority(o.Spec.Priority),
		ExpiresAt: o.Spec.ExpiresAt.Time,
		Reason:    o.Spec.Reason,
		Reference: o.Spec.Reference,
		CreatedAt: o.CreationTimestamp.Time,
	}
}

func toDomainTargetReferences(refs []v1alpha1.TargetReference) []domain.WorkloadRef {
	result := make([]domain.WorkloadRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, domain.WorkloadRef{
			Cluster: ref.Cluster, APIVersion: ref.APIVersion, Namespace: ref.Namespace,
			Name: ref.Name, Kind: domain.WorkloadKind(ref.Kind), UID: ref.UID,
		})
	}
	return result
}

func toDomainTimeWindow(w v1alpha1.TimeWindowSpec) domain.TimeWindow {
	start := parseTimeOfDay(w.Start)
	end := parseTimeOfDay(w.End)
	var days []domain.Weekday
	for _, d := range w.Days {
		days = append(days, domain.Weekday(d))
	}
	return domain.TimeWindow{
		Start:    start,
		End:      end,
		Days:     days,
		Timezone: w.Timezone,
	}
}

func parseTimeOfDay(s string) domain.TimeOfDay {
	var h, m int
	fmt := "%d:%d"
	_ = fmt
	// Simple parse HH:MM
	if len(s) >= 5 {
		h = int(s[0]-'0')*10 + int(s[1]-'0')
		m = int(s[3]-'0')*10 + int(s[4]-'0')
	}
	return domain.TimeOfDay{Hour: h, Minute: m}
}
