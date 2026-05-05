package informer

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ExitClaimController syncs ExitClaim → state.Cluster.
type ExitClaimController struct {
	client.Client
	Cluster *state.Cluster
}

func (r *ExitClaimController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var claim v1alpha1.ExitClaim
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		if apierrors.IsNotFound(err) {
			r.Cluster.DeleteExit(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.Cluster.UpdateExit(&claim)
	return ctrl.Result{}, nil
}

func (r *ExitClaimController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("informer-exitclaim").
		For(&v1alpha1.ExitClaim{}).
		Complete(r)
}
