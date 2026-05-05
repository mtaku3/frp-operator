package servicewatcher

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// ReverseSync watches Tunnel and updates the parent Service's
// Status.LoadBalancer.Ingress when AssignedIP changes. The forward
// translation (Controller) and this reverse path are split because they
// have different watch sources and different write surfaces (Tunnel.Spec
// vs Service.Status).
type ReverseSync struct {
	Client client.Client
}

// Reconcile patches Service.Status.LoadBalancer.Ingress[0].IP from
// Tunnel.Status.AssignedIP. No-ops when the parent Service doesn't
// exist or doesn't carry our LoadBalancerClass.
func (r *ReverseSync) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var t v1alpha1.Tunnel
	if err := r.Client.Get(ctx, req.NamespacedName, &t); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if t.Status.AssignedIP == "" {
		return reconcile.Result{}, nil
	}

	var svc corev1.Service
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: t.Namespace, Name: t.Name}, &svc); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if !isOurService(&svc) {
		return reconcile.Result{}, nil
	}

	desired := []corev1.LoadBalancerIngress{{IP: t.Status.AssignedIP}}
	if loadBalancerEqual(svc.Status.LoadBalancer.Ingress, desired) {
		return reconcile.Result{}, nil
	}

	patch := client.MergeFrom(svc.DeepCopy())
	svc.Status.LoadBalancer.Ingress = desired
	return reconcile.Result{}, r.Client.Status().Patch(ctx, &svc, patch)
}

// SetupWithManager registers the Tunnel watch.
func (r *ReverseSync) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("servicewatcher-reverse-sync").
		For(&v1alpha1.Tunnel{}).
		Complete(r)
}

func loadBalancerEqual(a, b []corev1.LoadBalancerIngress) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].IP != b[i].IP || a[i].Hostname != b[i].Hostname {
			return false
		}
	}
	return true
}
