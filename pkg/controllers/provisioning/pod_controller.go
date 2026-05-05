package provisioning

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// PodController watches Tunnels and feeds the Provisioner's batcher when
// a tunnel needs scheduling. The "pod" naming mirrors Karpenter's
// equivalent (which watches v1.Pod) — in this operator the analog is
// Tunnel.
type PodController struct {
	client.Client
	Batcher *Batcher[types.UID]
}

// Reconcile triggers the batcher whenever a tunnel is unscheduled or
// stuck in Allocating.
func (r *PodController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var t v1alpha1.Tunnel
	if err := r.Get(ctx, req.NamespacedName, &t); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if t.Status.AssignedExit == "" || t.Status.Phase == v1alpha1.TunnelPhaseAllocating {
		r.Batcher.Trigger(t.UID)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller.
func (r *PodController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("provisioning-pod").
		For(&v1alpha1.Tunnel{}).
		Complete(r)
}
