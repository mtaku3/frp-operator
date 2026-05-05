package informer

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

type TunnelController struct {
	client.Client
	Cluster *state.Cluster
}

func (r *TunnelController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var t v1alpha1.Tunnel
	key := state.TunnelKey(fmt.Sprintf("%s/%s", req.Namespace, req.Name))
	if err := r.Get(ctx, req.NamespacedName, &t); err != nil {
		if apierrors.IsNotFound(err) {
			r.Cluster.DeleteTunnelBinding(key)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.Cluster.UpdateTunnelBinding(key, t.Status.AssignedExit, t.Status.AssignedPorts)
	return ctrl.Result{}, nil
}

func (r *TunnelController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("informer-tunnel").
		For(&v1alpha1.Tunnel{}).
		Complete(r)
}
