// Package counter rolls up the resources reported by every child
// ExitClaim into ExitPool.Status.Resources and Status.Exits. The
// scheduler's poolLimitsExceeded reads StatePool.Resources, which the
// state cluster mirrors from this status surface.
package counter

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Controller writes Pool.Status.{Exits,Resources}. Idempotent.
type Controller struct {
	Client client.Client
}

// Reconcile lists every claim labelled with this pool, sums each
// claim's Status.Allocatable into the pool's Status.Resources, and
// adds a frp.operator.io/exits dimension equal to the count.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var pool v1alpha1.ExitPool
	if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if pool.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	var claims v1alpha1.ExitClaimList
	if err := r.Client.List(ctx, &claims,
		client.MatchingLabels{v1alpha1.LabelExitPool: pool.Name}); err != nil {
		return reconcile.Result{}, err
	}

	resources := corev1.ResourceList{}
	var exits int64
	for i := range claims.Items {
		c := &claims.Items[i]
		if c.DeletionTimestamp != nil {
			continue
		}
		exits++
		for k, v := range c.Status.Allocatable {
			if cur, ok := resources[k]; ok {
				cur.Add(v)
				resources[k] = cur
			} else {
				resources[k] = v.DeepCopy()
			}
		}
	}
	resources[corev1.ResourceName(v1alpha1.ResourceExits)] = *resource.NewQuantity(exits, resource.DecimalSI)

	if pool.Status.Exits == exits && resourceListsEqual(pool.Status.Resources, resources) {
		return reconcile.Result{}, nil
	}

	patch := client.MergeFrom(pool.DeepCopy())
	pool.Status.Exits = exits
	pool.Status.Resources = resources
	if err := r.Client.Status().Patch(ctx, &pool, patch); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// SetupWithManager registers the controller and watches ExitClaim with
// a label-keyed mapping back to its owning ExitPool.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("exitpool-counter").
		For(&v1alpha1.ExitPool{}).
		Watches(
			&v1alpha1.ExitClaim{},
			handler.EnqueueRequestsFromMapFunc(claimToPool),
		).
		Complete(r)
}

// claimToPool maps an ExitClaim event back to its owning ExitPool via
// the label.
func claimToPool(_ context.Context, obj client.Object) []reconcile.Request {
	poolName := obj.GetLabels()[v1alpha1.LabelExitPool]
	if poolName == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: poolName}}}
}

// resourceListsEqual reports semantic equality, treating zero-valued
// quantities and nil maps as equivalent for absent keys.
func resourceListsEqual(a, b corev1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if va.Cmp(vb) != 0 {
			return false
		}
	}
	return true
}
