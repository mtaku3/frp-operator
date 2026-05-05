package lifecycle

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
)

// Registrar implements Phase 2: probe the frps admin API.
type Registrar struct {
	KubeClient   client.Client
	AdminFactory func(baseURL string) *admin.Client
}

// Reconcile probes <PublicIP>:<AdminPort>/api/serverinfo. On success the
// claim transitions to Registered=True (and FrpsVersion is stamped from
// the response). On unreachable the condition transitions to False with
// reason AdminAPIUnreachable and a 10s requeue is requested.
func (r *Registrar) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
	if !isCondTrue(claim, v1alpha1.ConditionTypeLaunched) {
		return reconcile.Result{}, nil
	}
	if isCondTrue(claim, v1alpha1.ConditionTypeRegistered) {
		return reconcile.Result{}, nil
	}
	if claim.Status.PublicIP == "" {
		return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
	}
	adminPort := claim.Spec.Frps.AdminPort
	if adminPort == 0 {
		adminPort = 7400
	}
	factory := r.AdminFactory
	if factory == nil {
		factory = admin.New
	}
	orig := claim.DeepCopy()
	c := factory(fmt.Sprintf("http://%s:%d", claim.Status.PublicIP, adminPort))
	info, err := c.GetServerInfo(ctx)
	if err != nil {
		setCond(claim, v1alpha1.ConditionTypeRegistered, metav1.ConditionFalse,
			v1alpha1.ReasonAdminAPIUnreachable, err.Error())
		_ = r.KubeClient.Status().Patch(ctx, claim, client.MergeFrom(orig))
		return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if info != nil && info.Version != "" {
		claim.Status.FrpsVersion = info.Version
	}
	setCond(claim, v1alpha1.ConditionTypeRegistered, metav1.ConditionTrue,
		v1alpha1.ReasonReconciled, "admin API reachable")
	if err := r.KubeClient.Status().Patch(ctx, claim, client.MergeFrom(orig)); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{Requeue: true}, nil
}
