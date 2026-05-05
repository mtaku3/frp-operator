package lifecycle

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// DefaultRegistrationTTL is the upper bound on time spent waiting for a
// launched exit to register with the admin API before liveness disrupts
// it. Operators can override per-Controller via Controller.RegistrationTTL.
const DefaultRegistrationTTL = 15 * time.Minute

// Launcher implements Phase 1: it asks the configured CloudProvider to
// realize the claim and copies the hydrated status onto the live object.
type Launcher struct {
	KubeClient    client.Client
	CloudProvider *cloudprovider.Registry
}

// Reconcile invokes cloudProvider.Create when the claim has no
// Launched=True condition yet. Side-effects:
//   - On success: hydrates Status (ProviderID, ExitName, ImageID, FrpsVersion,
//     Capacity, Allocatable, PublicIP), patches status, sets
//     Conditions[Launched]=True, requests requeue.
//   - On provider error: sets Conditions[Launched]=False with reason
//     ProviderError, persists status, requeues after 30s.
func (l *Launcher) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	if isCondTrue(claim, v1alpha1.ConditionTypeLaunched) {
		return reconcile.Result{}, nil
	}

	cp, err := l.CloudProvider.For(claim.Spec.ProviderClassRef.Kind)
	if err != nil {
		setCond(claim, v1alpha1.ConditionTypeLaunched, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
		if uerr := l.KubeClient.Status().Update(ctx, claim); uerr != nil {
			return reconcile.Result{}, fmt.Errorf("status update: %w", uerr)
		}
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hydrated, err := cp.Create(ctx, claim)
	if err != nil {
		setCond(claim, v1alpha1.ConditionTypeLaunched, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
		_ = l.KubeClient.Status().Update(ctx, claim)
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Copy hydrated status into the live object, preserving any conditions
	// already accumulated by earlier reconciles.
	conditions := claim.Status.Conditions
	claim.Status = hydrated.Status
	claim.Status.Conditions = conditions
	setCond(claim, v1alpha1.ConditionTypeLaunched, metav1.ConditionTrue, v1alpha1.ReasonProvisioned, "exit launched")
	if err := l.KubeClient.Status().Update(ctx, claim); err != nil {
		return reconcile.Result{}, fmt.Errorf("status update: %w", err)
	}
	return reconcile.Result{Requeue: true}, nil
}
