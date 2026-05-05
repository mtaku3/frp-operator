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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
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
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var pool v1alpha1.ExitPool
	if err := r.Client.Get(ctx, req.NamespacedName, &pool); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if pool.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	original := pool.DeepCopy()
	ref := pool.Spec.Template.Spec.ProviderClassRef

	factory, known := r.KindToObject[ref.Kind]
	switch {
	case !known:
		setCond(&pool, v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse,
			v1alpha1.ReasonProviderClassNotFound,
			"kind "+ref.Kind+" is not registered with any provider")
	default:
		obj := factory()
		err := r.Client.Get(ctx, types.NamespacedName{Name: ref.Name}, obj)
		switch {
		case apierrors.IsNotFound(err):
			setCond(&pool, v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse,
				v1alpha1.ReasonProviderClassNotFound,
				ref.Kind+"/"+ref.Name+" not found")
		case err != nil:
			setCond(&pool, v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionFalse,
				v1alpha1.ReasonProviderError, err.Error())
		default:
			setCond(&pool, v1alpha1.ConditionTypeProviderClassReady, metav1.ConditionTrue,
				v1alpha1.ReasonReconciled, "")
		}
	}

	// Roll Ready up from ProviderClassReady. Phase 7 keeps this
	// conjunction simple; a future ready-reducer can fold in
	// ValidationSucceeded and other surface conditions.
	if apimeta.IsStatusConditionTrue(pool.Status.Conditions, v1alpha1.ConditionTypeProviderClassReady) {
		setCond(&pool, v1alpha1.ConditionTypeReady, metav1.ConditionTrue,
			v1alpha1.ReasonReconciled, "")
	} else {
		setCond(&pool, v1alpha1.ConditionTypeReady, metav1.ConditionFalse,
			v1alpha1.ReasonNotReady, "ProviderClassReady is not True")
	}

	patch := client.MergeFrom(original)
	if err := r.Client.Status().Patch(ctx, &pool, patch); err != nil {
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

func setCond(pool *v1alpha1.ExitPool, t string, status metav1.ConditionStatus, reason, msg string) {
	apimeta.SetStatusCondition(&pool.Status.Conditions, metav1.Condition{
		Type:               t,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}
