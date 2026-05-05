package informer

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ExitPoolController syncs ExitPool → state.Cluster.
//
// The Reconcile body intentionally mirrors ExitClaimController's: both
// are write-only translators from one CRD into a Cluster cache method.
// Extracting them into a generic helper would obscure the per-type
// `client.Client` embed and the typed Cluster method calls without
// reducing complexity meaningfully.
type ExitPoolController struct {
	client.Client
	Cluster *state.Cluster
}

//nolint:dupl // see comment on the type — intentional twin of exitclaim_controller.go.
func (r *ExitPoolController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pool v1alpha1.ExitPool
	if err := r.Get(ctx, req.NamespacedName, &pool); err != nil {
		if apierrors.IsNotFound(err) {
			r.Cluster.DeletePool(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.Cluster.UpdatePool(&pool)
	return ctrl.Result{}, nil
}

func (r *ExitPoolController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("informer-exitpool").
		For(&v1alpha1.ExitPool{}).
		Complete(r)
}
