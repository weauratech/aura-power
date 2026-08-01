package reconciler

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
	"github.com/weauratech/aura-power/internal/ports"
)

// OverrideReconciler reconciles PowerOverride objects.
// Handles expiration lifecycle and enqueues affected targets.
type OverrideReconciler struct {
	client.Client
	Audit ports.AuditRecorder
}

func (r *OverrideReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("override", req.NamespacedName)

	var override v1alpha1.PowerOverride
	if err := r.Get(ctx, req.NamespacedName, &override); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	now := time.Now()

	// Check expiration
	if override.Spec.ExpiresAt.Time.Before(now) {
		// Override has expired
		if override.Status.Phase != "Expired" {
			override.Status.Phase = "Expired"
			override.Status.ExpiresIn = "expired"

			// Update condition
			meta := metav1.Condition{
				Type:               "Active",
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
				Reason:             "Expired",
				Message:            "Override has expired",
			}
			override.Status.Conditions = []metav1.Condition{meta}

			if err := r.Status().Update(ctx, &override); err != nil {
				return ctrl.Result{}, err
			}

			logger.Info("override expired", "expiresAt", override.Spec.ExpiresAt.Time)
		}
		return ctrl.Result{}, nil // No requeue needed for expired overrides
	}

	// Override is active
	timeUntilExpiration := override.Spec.ExpiresAt.Time.Sub(now)
	override.Status.Phase = "Active"
	override.Status.ExpiresIn = fmt.Sprintf("%s", timeUntilExpiration.Round(time.Minute))

	// Update condition
	meta := metav1.Condition{
		Type:               "Active",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "Active",
		Message:            fmt.Sprintf("Override active, expires in %s", timeUntilExpiration.Round(time.Minute)),
	}
	override.Status.Conditions = []metav1.Condition{meta}

	if err := r.Status().Update(ctx, &override); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("override active", "expiresIn", timeUntilExpiration.Round(time.Minute))

	// Requeue at expiration time
	return ctrl.Result{RequeueAfter: timeUntilExpiration}, nil
}

func (r *OverrideReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.PowerOverride{}).
		Complete(r)
}
