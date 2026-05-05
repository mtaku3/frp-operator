package servicewatcher

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// LoadBalancerClass is the value of corev1.Service.Spec.LoadBalancerClass
// this controller acts on.
const LoadBalancerClass = v1alpha1.Group + "/frp"

// LabelManagedByService stamps Tunnels created by this controller so
// other controllers / operators can identify them.
const LabelManagedByService = v1alpha1.Group + "/managed-by-service"

// Controller translates a corev1.Service of LoadBalancerClass into a
// sibling Tunnel CR (same namespace + name).
type Controller struct {
	Client client.Client
}

// Reconcile materializes the sibling Tunnel for our-class Services and
// deletes it on Service teardown.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var svc corev1.Service
	err := r.Client.Get(ctx, req.NamespacedName, &svc)
	if apierrors.IsNotFound(err) {
		// Service is gone. Garbage-collect any sibling Tunnel we own
		// at the same NamespacedName.
		return r.deleteSiblingIfOurs(ctx, req.Namespace, req.Name)
	}
	if err != nil {
		return reconcile.Result{}, err
	}

	if !isOurService(&svc) {
		return reconcile.Result{}, nil
	}

	if !svc.DeletionTimestamp.IsZero() {
		return r.deleteSiblingIfOurs(ctx, svc.Namespace, svc.Name)
	}
	return r.upsertSibling(ctx, &svc)
}

func isOurService(svc *corev1.Service) bool {
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return false
	}
	if svc.Spec.LoadBalancerClass == nil || *svc.Spec.LoadBalancerClass != LoadBalancerClass {
		return false
	}
	return true
}

func (r *Controller) upsertSibling(ctx context.Context, svc *corev1.Service) (reconcile.Result, error) {
	spec, err := ParseAnnotations(svc)
	if err != nil {
		return reconcile.Result{}, err
	}
	spec.Ports = PortsFromService(svc)

	desiredAnnotations := copyDoNotDisrupt(svc.Annotations)
	desiredLabels := map[string]string{LabelManagedByService: "true"}

	var existing v1alpha1.Tunnel
	err = r.Client.Get(ctx, types.NamespacedName{Namespace: svc.Namespace, Name: svc.Name}, &existing)
	if err != nil && !apierrors.IsNotFound(err) {
		return reconcile.Result{}, err
	}
	if apierrors.IsNotFound(err) {
		t := &v1alpha1.Tunnel{
			ObjectMeta: metav1.ObjectMeta{
				Name:        svc.Name,
				Namespace:   svc.Namespace,
				Labels:      desiredLabels,
				Annotations: desiredAnnotations,
			},
			Spec: spec,
		}
		return reconcile.Result{}, r.Client.Create(ctx, t)
	}

	if specEqual(existing.Spec, spec) && annotationsEqual(existing.Annotations, desiredAnnotations) {
		return reconcile.Result{}, nil
	}
	existing.Spec = spec
	// Merge our managed annotation into the existing map rather than
	// replacing the whole map — other controllers (e.g. Phase 7
	// exitpool/hash) may set their own annotations on the Tunnel and
	// must survive a Service-driven update.
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	if v, ok := desiredAnnotations[v1alpha1.AnnotationDoNotDisrupt]; ok {
		existing.Annotations[v1alpha1.AnnotationDoNotDisrupt] = v
	} else {
		delete(existing.Annotations, v1alpha1.AnnotationDoNotDisrupt)
	}
	// Preserve the management label even if missing.
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	existing.Labels[LabelManagedByService] = "true"
	return reconcile.Result{}, r.Client.Update(ctx, &existing)
}

// deleteSiblingIfOurs deletes the same-name Tunnel iff we created it
// (carries LabelManagedByService). Bare Tunnels created by other actors
// at the same NamespacedName must not be touched.
func (r *Controller) deleteSiblingIfOurs(ctx context.Context, namespace, name string) (reconcile.Result, error) {
	var t v1alpha1.Tunnel
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &t); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if t.Labels[LabelManagedByService] != "true" {
		return reconcile.Result{}, nil
	}
	if !t.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, nil
	}
	return reconcile.Result{}, client.IgnoreNotFound(r.Client.Delete(ctx, &t))
}

// copyDoNotDisrupt returns nil when the Service does not carry the
// do-not-disrupt opt-out, or a single-entry map carrying it forward to
// the Tunnel. Other annotations on the Service are not propagated -
// they are tunnel-config inputs, not metadata to mirror.
func copyDoNotDisrupt(a map[string]string) map[string]string {
	v, ok := a[v1alpha1.AnnotationDoNotDisrupt]
	if !ok || v == "" {
		return nil
	}
	return map[string]string{v1alpha1.AnnotationDoNotDisrupt: v}
}

func specEqual(a, b v1alpha1.TunnelSpec) bool { return reflect.DeepEqual(a, b) }

func annotationsEqual(a, b map[string]string) bool {
	// Compare only over the keys we manage. Today that's just AnnotationDoNotDisrupt.
	return a[v1alpha1.AnnotationDoNotDisrupt] == b[v1alpha1.AnnotationDoNotDisrupt]
}

// SetupWithManager registers the Service watch.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("servicewatcher").
		For(&corev1.Service{}).
		Complete(r)
}
