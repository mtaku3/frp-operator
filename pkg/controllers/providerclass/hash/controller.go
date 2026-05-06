package hash

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Controller reconciles one ProviderClass kind. Computes SpecHash and
// stamps it on:
//
//  1. The ProviderClass object itself.
//  2. Every ExitPool whose Spec.Template.Spec.ProviderClassRef points at it.
//  3. Every ExitClaim whose Spec.ProviderClassRef points at it.
//
// Drift compares the hash on the pool against the hash on the claim;
// a mismatch triggers replacement. One Controller instance per
// ProviderClass kind — operator wiring instantiates from
// cloudprovider.Registry.
type Controller struct {
	Client client.Client
	// Watch is an empty instance of the ProviderClass type this
	// controller reconciles (e.g. &dov1alpha1.DigitalOceanProviderClass{}).
	Watch client.Object
	// Kind matches ProviderClassRef.Kind (e.g. "DigitalOceanProviderClass").
	Kind string
}

// Reconcile is idempotent: if the PC already carries the current hash
// AND every referencing pool + claim does too, this is a no-op.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	obj := r.Watch.DeepCopyObject().(client.Object)
	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if obj.GetDeletionTimestamp() != nil {
		return reconcile.Result{}, nil
	}

	h, err := SpecHash(obj)
	if err != nil {
		return reconcile.Result{}, err
	}

	if err := r.stampObject(ctx, obj, h); err != nil {
		if apierrors.IsConflict(err) {
			return reconcile.Result{Requeue: true}, nil
		}
		return reconcile.Result{}, err
	}
	if err := r.stampPools(ctx, obj.GetName(), h); err != nil {
		return reconcile.Result{}, err
	}
	if err := r.stampClaims(ctx, obj.GetName(), h); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (r *Controller) stampObject(ctx context.Context, obj client.Object, h string) error {
	if obj.GetAnnotations()[v1alpha1.AnnotationProviderClassHash] == h {
		return nil
	}
	patched := obj.DeepCopyObject().(client.Object)
	ann := patched.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[v1alpha1.AnnotationProviderClassHash] = h
	patched.SetAnnotations(ann)
	return r.Client.Patch(ctx, patched, client.MergeFrom(obj))
}

func (r *Controller) stampPools(ctx context.Context, pcName, h string) error {
	var pools v1alpha1.ExitPoolList
	if err := r.Client.List(ctx, &pools); err != nil {
		return err
	}
	for i := range pools.Items {
		p := &pools.Items[i]
		if p.DeletionTimestamp != nil {
			continue
		}
		ref := p.Spec.Template.Spec.ProviderClassRef
		if ref.Kind != r.Kind || ref.Name != pcName {
			continue
		}
		if p.Annotations[v1alpha1.AnnotationProviderClassHash] == h {
			continue
		}
		patch := client.MergeFrom(p.DeepCopy())
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}
		p.Annotations[v1alpha1.AnnotationProviderClassHash] = h
		if err := r.Client.Patch(ctx, p, patch); err != nil {
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func (r *Controller) stampClaims(ctx context.Context, pcName, h string) error {
	var claims v1alpha1.ExitClaimList
	if err := r.Client.List(ctx, &claims); err != nil {
		return err
	}
	for i := range claims.Items {
		c := &claims.Items[i]
		if c.DeletionTimestamp != nil {
			continue
		}
		ref := c.Spec.ProviderClassRef
		if ref.Kind != r.Kind || ref.Name != pcName {
			continue
		}
		if c.Annotations[v1alpha1.AnnotationProviderClassHash] == h {
			continue
		}
		patch := client.MergeFrom(c.DeepCopy())
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		c.Annotations[v1alpha1.AnnotationProviderClassHash] = h
		if err := r.Client.Patch(ctx, c, patch); err != nil {
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// SetupWithManager registers the controller. Watches the ProviderClass
// type AND ExitPool/ExitClaim — when a fresh pool/claim picks up a PC
// after the PC's last reconcile, we re-enqueue the PC to stamp the
// child.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	if r.Kind == "" {
		return fmt.Errorf("providerclass-hash: Kind is required")
	}
	return ctrl.NewControllerManagedBy(mgr).
		Named("providerclass-hash-"+strings.ToLower(r.Kind)).
		For(r.Watch).
		Watches(&v1alpha1.ExitPool{}, handler.EnqueueRequestsFromMapFunc(r.poolToPC)).
		Watches(&v1alpha1.ExitClaim{}, handler.EnqueueRequestsFromMapFunc(r.claimToPC)).
		Complete(r)
}

func (r *Controller) poolToPC(_ context.Context, obj client.Object) []reconcile.Request {
	p, ok := obj.(*v1alpha1.ExitPool)
	if !ok {
		return nil
	}
	if p.Spec.Template.Spec.ProviderClassRef.Kind != r.Kind {
		return nil
	}
	name := p.Spec.Template.Spec.ProviderClassRef.Name
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: name}}}
}

func (r *Controller) claimToPC(_ context.Context, obj client.Object) []reconcile.Request {
	c, ok := obj.(*v1alpha1.ExitClaim)
	if !ok {
		return nil
	}
	if c.Spec.ProviderClassRef.Kind != r.Kind {
		return nil
	}
	name := c.Spec.ProviderClassRef.Name
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: name}}}
}

// Ensure runtime.Object check at compile time.
var _ runtime.Object = (*v1alpha1.ExitPool)(nil)
