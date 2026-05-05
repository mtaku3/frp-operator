// Package readiness resolves an ExitPool's ProviderClassRef to a real
// ProviderClass instance. It surfaces the result on
// Conditions[ProviderClassReady] and rolls Conditions[Ready] up from
// it (Ready=True iff ProviderClassReady=True). Phase 9 will register
// every cloudprovider's GetSupportedProviderClasses-returned kinds in
// the KindToObject map at startup.
package readiness

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/conditions"
)

// Controller resolves Pool.Spec.Template.Spec.ProviderClassRef.
type Controller struct {
	Client client.Client
	// KindToObject maps a ProviderClassRef.Kind ("LocalDockerProviderClass")
	// to a factory yielding a fresh empty typed object. Phase 9 wiring
	// populates it from the cloudprovider Registry's
	// GetSupportedProviderClasses outputs.
	KindToObject map[string]func() client.Object
}

// Reconcile sets Conditions[ProviderClassReady] and Conditions[Ready].
// Each condition is patched with a JSON Patch op so that other
// controllers writing different Types on the same object do not
// clobber each other (the previous MergeFrom path replaced the whole
// conditions array on every write).
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var pool v1alpha1.ExitPool
	if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if pool.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	ref := pool.Spec.Template.Spec.ProviderClassRef
	factory, known := r.KindToObject[ref.Kind]

	var pcrCond metav1.Condition
	switch {
	case !known:
		pcrCond = conditions.MakeCondition(pool.Status.Conditions,
			v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse,
			pool.Generation, v1alpha1.ReasonProviderClassNotFound,
			"kind "+ref.Kind+" is not registered with any provider")
	default:
		obj := factory()
		err := r.Client.Get(ctx, types.NamespacedName{Name: ref.Name}, obj)
		switch {
		case apierrors.IsNotFound(err):
			pcrCond = conditions.MakeCondition(pool.Status.Conditions,
				v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse,
				pool.Generation, v1alpha1.ReasonProviderClassNotFound,
				ref.Kind+"/"+ref.Name+" not found")
		case err != nil:
			pcrCond = conditions.MakeCondition(pool.Status.Conditions,
				v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse,
				pool.Generation, v1alpha1.ReasonProviderError, err.Error())
		default:
			pcrCond = conditions.MakeCondition(pool.Status.Conditions,
				v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionTrue,
				pool.Generation, v1alpha1.ReasonReconciled, "")
		}
	}

	if err := conditions.PatchCondition(ctx, r.Client, &pool, pool.Status.Conditions, pcrCond); err != nil {
		return reconcile.Result{}, err
	}

	// Re-fetch so the Ready rollup observes the just-patched
	// ProviderClassReady alongside whatever other writers committed.
	if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Roll Ready up from ProviderClassReady. Phase 7 keeps this
	// conjunction simple; a future ready-reducer can fold in
	// ValidationSucceeded and other surface conditions.
	var readyCond metav1.Condition
	if isTrue(pool.Status.Conditions, v1alpha1.ConditionTypeProviderClassReady) {
		readyCond = conditions.MakeCondition(pool.Status.Conditions,
			v1alpha1.ConditionTypeReady, metav1.ConditionTrue,
			pool.Generation, v1alpha1.ReasonReconciled, "")
	} else {
		readyCond = conditions.MakeCondition(pool.Status.Conditions,
			v1alpha1.ConditionTypeReady, metav1.ConditionFalse,
			pool.Generation, v1alpha1.ReasonNotReady, "ProviderClassReady is not True")
	}
	if err := conditions.PatchCondition(ctx, r.Client, &pool, pool.Status.Conditions, readyCond); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// SetupWithManager registers the controller.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("exitpool-readiness").
		For(&v1alpha1.ExitPool{}).
		Complete(r)
}

func isTrue(conds []metav1.Condition, t string) bool {
	for _, c := range conds {
		if c.Type == t {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}
