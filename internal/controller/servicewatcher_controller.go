package controller

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// ServiceWatcherReconciler watches LoadBalancer Services with our class and
// drives a sibling Tunnel CR. Reverse-syncs Tunnel.Status.AssignedIP into
// Service.status.loadBalancer.ingress so kubectl shows it as an external IP.
type ServiceWatcherReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives one Service.
func (r *ServiceWatcherReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var svc corev1.Service
	if err := r.Get(ctx, req.NamespacedName, &svc); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !serviceMatchesClass(&svc) {
		return ctrl.Result{}, nil
	}
	if !svc.DeletionTimestamp.IsZero() {
		// Service is being deleted; the owner-ref ensures the Tunnel
		// gets garbage-collected. Nothing to do here.
		return ctrl.Result{}, nil
	}

	desiredSpec, err := translateServiceToTunnelSpec(&svc)
	if err != nil {
		logger.Error(err, "translate Service to TunnelSpec")
		return ctrl.Result{}, err
	}
	// CRD requires schedulingPolicyRef.name be at least 1 char. Fall back
	// to "default" when the user didn't pin a policy via annotation.
	if desiredSpec.SchedulingPolicyRef.Name == "" {
		desiredSpec.SchedulingPolicyRef.Name = "default"
	}

	// Reconcile the sibling Tunnel.
	var tunnel frpv1alpha1.Tunnel
	err = r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, &tunnel)
	if apierrors.IsNotFound(err) {
		tunnel = frpv1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svc.Name,
				Namespace: svc.Namespace,
				Labels:    map[string]string{"frp-operator.io/created-by": "service-watcher"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "v1",
					Kind:               "Service",
					Name:               svc.Name,
					UID:                svc.UID,
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
				}},
			},
			Spec: desiredSpec,
		}
		if err := r.Create(ctx, &tunnel); err != nil {
			return ctrl.Result{}, fmt.Errorf("create Tunnel: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("get Tunnel: %w", err)
	}

	// Drift: update spec if changed.
	if !reflect.DeepEqual(tunnel.Spec, desiredSpec) {
		tunnel.Spec = desiredSpec
		if err := r.Update(ctx, &tunnel); err != nil {
			return ctrl.Result{}, fmt.Errorf("update Tunnel: %w", err)
		}
	}

	// Reverse-sync: write the assigned IP into Service.status if the tunnel
	// has one and is at least Connecting.
	if tunnel.Status.AssignedIP != "" &&
		(tunnel.Status.Phase == frpv1alpha1.TunnelConnecting ||
			tunnel.Status.Phase == frpv1alpha1.TunnelReady) {
		return r.syncIngressStatus(ctx, &svc, tunnel.Status.AssignedIP)
	}
	return ctrl.Result{}, nil
}

// syncIngressStatus writes ip into svc.status.loadBalancer.ingress[] if it
// isn't already there.
func (r *ServiceWatcherReconciler) syncIngressStatus(ctx context.Context, svc *corev1.Service, ip string) (ctrl.Result, error) {
	for _, in := range svc.Status.LoadBalancer.Ingress {
		if in.IP == ip {
			return ctrl.Result{}, nil
		}
	}
	patch := client.MergeFrom(svc.DeepCopy())
	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: ip}}
	if err := r.Status().Patch(ctx, svc, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch service status: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager wires the controller. Watches Services and Tunnels (so
// Tunnel status changes trigger Service status reverse-sync).
func (r *ServiceWatcherReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Owns(&frpv1alpha1.Tunnel{}).
		Named("servicewatcher").
		Complete(r)
}
