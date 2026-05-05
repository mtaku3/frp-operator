package provisioning

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// NodeController watches ExitClaims and feeds the Provisioner's batcher
// whenever an exit becomes Ready, drifts, or vanishes — all of which
// can unblock pending tunnels.
type NodeController struct {
	client.Client
	Batcher *Batcher[types.UID]
}

// Reconcile triggers the batcher on every ExitClaim event.
func (r *NodeController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var c v1alpha1.ExitClaim
	if err := r.Get(ctx, req.NamespacedName, &c); err != nil {
		if apierrors.IsNotFound(err) {
			// Vanished claim still warrants a re-Solve.
			r.Batcher.Trigger(types.UID(req.Name))
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.Batcher.Trigger(c.UID)
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller.
func (r *NodeController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("provisioning-node").
		For(&v1alpha1.ExitClaim{}).
		Complete(r)
}
