package lifecycle

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Liveness implements Phase 4: if the claim has been Launched but never
// reaches Registered within RegistrationTTL, mark Disrupted and delete it
// so the provisioner can take another shot.
type Liveness struct {
	KubeClient client.Client
	// Now is overridable in tests.
	Now func() time.Time
	// RegistrationTTL bounds the wait between Launched and Registered.
	// Zero falls back to DefaultRegistrationTTL.
	RegistrationTTL time.Duration
}

func (l *Liveness) ttl() time.Duration {
	if l.RegistrationTTL > 0 {
		return l.RegistrationTTL
	}
	return DefaultRegistrationTTL
}

func (l *Liveness) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Liveness) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	if !isCondTrue(claim, v1alpha1.ConditionTypeLaunched) {
		return reconcile.Result{}, nil
	}
	if isCondTrue(claim, v1alpha1.ConditionTypeRegistered) {
		return reconcile.Result{}, nil
	}
	launchedAt := condTransitionTime(claim, v1alpha1.ConditionTypeLaunched)
	if launchedAt.IsZero() {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if l.now().Sub(launchedAt) < l.ttl() {
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}
	orig := claim.DeepCopy()
	setCond(claim, v1alpha1.ConditionTypeDisrupted, metav1.ConditionTrue,
		v1alpha1.ReasonRegistrationTimeout, "exceeded RegistrationTTL")
	_ = l.KubeClient.Status().Patch(ctx, claim, client.MergeFrom(orig))
	if err := l.KubeClient.Delete(ctx, claim); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}
