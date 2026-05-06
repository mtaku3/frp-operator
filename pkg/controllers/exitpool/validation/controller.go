// Package validation surfaces stateful spec validation as
// Conditions[ValidationSucceeded]. Most syntactic validation lives in
// CEL on the CRD; this controller covers checks that benefit from a
// reconcile loop (or that defend in depth against missing CEL).
package validation

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/conditions"
)

// Controller checks the pool spec for stateful invariants and writes
// Conditions[ValidationSucceeded].
type Controller struct {
	Client client.Client
}

// Reconcile validates a single pool. Failures set
// ValidationSucceeded=False with an explanatory message; everything
// passing yields True/Reconciled. The condition is patched with a
// JSON Patch op so concurrent writers to other condition Types don't
// clobber each other.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var pool v1alpha1.ExitPool
	if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if pool.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	var cond metav1.Condition
	if msg := validatePool(&pool); msg != "" {
		cond = conditions.MakeCondition(pool.Status.Conditions,
			v1alpha1.ConditionTypeValidationSucceeded, metav1.ConditionFalse,
			pool.Generation, v1alpha1.ReasonInvalidRequirements, msg)
	} else {
		cond = conditions.MakeCondition(pool.Status.Conditions,
			v1alpha1.ConditionTypeValidationSucceeded, metav1.ConditionTrue,
			pool.Generation, v1alpha1.ReasonReconciled, "")
	}

	if err := conditions.PatchCondition(ctx, r.Client, &pool, pool.Status.Conditions, cond); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// validatePool returns the empty string when the spec passes, or the
// failure reason when something is wrong. Add new checks here.
func validatePool(pool *v1alpha1.ExitPool) string {
	if pool.Spec.Replicas != nil && pool.Spec.Weight != nil {
		return "spec.replicas and spec.weight are mutually exclusive"
	}
	return ""
}

// SetupWithManager registers the controller.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("exitpool-validation").
		For(&v1alpha1.ExitPool{}).
		Complete(r)
}
