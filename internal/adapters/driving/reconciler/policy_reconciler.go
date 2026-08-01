package reconciler

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/core/domain"
	"github.com/weauratech/aura-power/internal/ports"
)

// PolicyReconciler reconciles PowerPolicy objects.
// On policy create/update/delete, it enqueues affected targets for re-evaluation.
type PolicyReconciler struct {
	client.Client
	Audit ports.AuditRecorder
}

func (r *PolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("policy", req.NamespacedName)

	var policy v1alpha1.PowerPolicy
	if err := r.Get(ctx, req.NamespacedName, &policy); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("policy deleted, targets will re-evaluate on next cycle")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("policy reconciled", "priority", policy.Spec.Priority, "desiredState", policy.Spec.Schedule.DesiredState)

	// Update affected target count
	targets, err := r.countAffectedTargets(ctx, &policy)
	if err == nil {
		policy.Status.AffectedTargets = int32(targets)
		_ = r.Status().Update(ctx, &policy)
	}

	return ctrl.Result{}, nil
}

func (r *PolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PowerPolicy{}).
		Complete(r)
}

func (r *PolicyReconciler) countAffectedTargets(ctx context.Context, policy *v1alpha1.PowerPolicy) (int, error) {
	var targets v1alpha1.PowerTargetList
	if err := r.List(ctx, &targets); err != nil {
		return 0, err
	}

	count := 0
	domainPolicy := toDomainPolicy(policy)
	for _, t := range targets.Items {
		domainTarget := toDomainTarget(&t)
		if matchesPolicyScope(domainTarget, domainPolicy) {
			count++
		}
	}
	return count, nil
}

func matchesPolicyScope(target domain.Target, policy domain.PolicySpec) bool {
	return domain.MatchesScope(target, policy.Scope)
}
