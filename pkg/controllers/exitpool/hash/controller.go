package hash

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Controller computes ExitPool.Spec.Template.Spec's canonical hash and
// stamps it on the pool's annotations + every child ExitClaim's
// annotations. Phase 6's Drift method reads the two annotations.
type Controller struct {
	Client client.Client
}

// Reconcile is idempotent: a no-op when the pool already carries the
// current hash AND every child claim does too.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var pool v1alpha1.ExitPool
	if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if pool.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	h, err := PoolTemplateHash(&pool)
	if err != nil {
		return reconcile.Result{}, err
	}

	// 1. Stamp on the pool itself.
	if pool.Annotations[v1alpha1.AnnotationPoolHash] != h {
		patch := client.MergeFrom(pool.DeepCopy())
		if pool.Annotations == nil {
			pool.Annotations = map[string]string{}
		}
		pool.Annotations[v1alpha1.AnnotationPoolHash] = h
		if err := r.Client.Patch(ctx, &pool, patch); err != nil {
			if apierrors.IsConflict(err) {
				// next reconcile retries with a fresh read
				return reconcile.Result{Requeue: true}, nil
			}
			return reconcile.Result{}, err
		}
	}

	// 2. Stamp on every child ExitClaim.
	var claims v1alpha1.ExitClaimList
	if err := r.Client.List(ctx, &claims,
		client.MatchingLabels{v1alpha1.LabelExitPool: pool.Name}); err != nil {
		return reconcile.Result{}, err
	}
	for i := range claims.Items {
		c := &claims.Items[i]
		if c.DeletionTimestamp != nil {
			continue
		}
		if c.Annotations[v1alpha1.AnnotationPoolHash] == h {
			continue
		}
		patch := client.MergeFrom(c.DeepCopy())
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		c.Annotations[v1alpha1.AnnotationPoolHash] = h
		if err := r.Client.Patch(ctx, c, patch); err != nil {
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				continue // next reconcile retries
			}
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

// SetupWithManager registers the controller with the supplied manager.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("exitpool-hash").
		For(&v1alpha1.ExitPool{}).
		Complete(r)
}
