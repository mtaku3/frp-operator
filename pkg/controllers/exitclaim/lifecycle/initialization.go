package lifecycle

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Initializer implements Phase 3: when Registered transitions True, mark
// Initialized + Ready. Future iterations may reserve admin/control ports
// here.
type Initializer struct {
	KubeClient client.Client
}

func (i *Initializer) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	if !isCondTrue(claim, v1alpha1.ConditionTypeRegistered) {
		return reconcile.Result{}, nil
	}
	if isCondTrue(claim, v1alpha1.ConditionTypeInitialized) && isCondTrue(claim, v1alpha1.ConditionTypeReady) {
		return reconcile.Result{}, nil
	}
	setCond(claim, v1alpha1.ConditionTypeInitialized, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "ready for tunnels")
	setCond(claim, v1alpha1.ConditionTypeReady, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "")
	return reconcile.Result{}, i.KubeClient.Status().Update(ctx, claim)
}
