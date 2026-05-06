// Package emptiness stamps the Empty condition on each Ready
// ExitClaim. Mirrors Karpenter's emptiness controller — a Ready
// NodeClaim with zero pods carries Empty=True; the disruption
// controller's Emptiness method then waits ConsolidateAfter before
// terminating.
//
// Without this controller, disruption.candidates falls back to
// CreationTimestamp, which means consolidation is "eager on first
// observation" rather than "fires after a tunnel-less window". With
// this controller, the LastTransitionTime of Empty=True is the
// authoritative since-when stamp.
package emptiness

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

const (
	reasonNoTunnels = "NoTunnels"
	reasonHasTunnel = "HasTunnel"
	reasonNotReady  = v1alpha1.ReasonNotReady
)

// Controller stamps the Empty condition on each Ready ExitClaim.
type Controller struct {
	Client client.Client
}

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (reconcile.Result, error) {
	var claim v1alpha1.ExitClaim
	if err := r.Client.Get(ctx, req.NamespacedName, &claim); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	if claim.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	// Pre-Ready claims have no meaningful "empty" — they're still
	// being launched. Karpenter does the same: emptiness is a
	// post-Initialize concept.
	if !isReady(&claim) {
		return r.setCondition(ctx, &claim, metav1.ConditionUnknown, reasonNotReady, "claim not yet Ready")
	}

	bound, err := r.countBoundTunnels(ctx, claim.Name)
	if err != nil {
		return reconcile.Result{}, err
	}
	if bound == 0 {
		return r.setCondition(ctx, &claim, metav1.ConditionTrue, reasonNoTunnels,
			"no tunnels currently bound to this exit")
	}
	return r.setCondition(ctx, &claim, metav1.ConditionFalse, reasonHasTunnel,
		"one or more tunnels are bound to this exit")
}

func (r *Controller) countBoundTunnels(ctx context.Context, claimName string) (int, error) {
	var tunnels v1alpha1.TunnelList
	if err := r.Client.List(ctx, &tunnels); err != nil {
		return 0, err
	}
	n := 0
	for i := range tunnels.Items {
		t := &tunnels.Items[i]
		if t.DeletionTimestamp != nil {
			continue
		}
		if t.Status.AssignedExit == claimName {
			n++
		}
	}
	return n, nil
}

// setCondition is idempotent: only patches when type/status/reason
// changes, preserving LastTransitionTime when status is unchanged.
func (r *Controller) setCondition(
	ctx context.Context, claim *v1alpha1.ExitClaim,
	status metav1.ConditionStatus, reason, message string,
) (reconcile.Result, error) {
	cur := findCondition(claim.Status.Conditions, v1alpha1.ConditionTypeEmpty)
	if cur != nil && cur.Status == status && cur.Reason == reason {
		return reconcile.Result{}, nil
	}
	patch := client.MergeFrom(claim.DeepCopy())
	now := metav1.Now()
	next := metav1.Condition{
		Type:               v1alpha1.ConditionTypeEmpty,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
	claim.Status.Conditions = upsertCondition(claim.Status.Conditions, next)
	if err := r.Client.Status().Patch(ctx, claim, patch); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func isReady(claim *v1alpha1.ExitClaim) bool {
	c := findCondition(claim.Status.Conditions, v1alpha1.ConditionTypeReady)
	return c != nil && c.Status == metav1.ConditionTrue
}

func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

func upsertCondition(conds []metav1.Condition, in metav1.Condition) []metav1.Condition {
	for i := range conds {
		if conds[i].Type == in.Type {
			// Preserve LastTransitionTime if status didn't change.
			if conds[i].Status == in.Status {
				in.LastTransitionTime = conds[i].LastTransitionTime
			}
			conds[i] = in
			return conds
		}
	}
	return append(conds, in)
}

// SetupWithManager registers the controller. Watches ExitClaim (For)
// and Tunnel — a tunnel binding/unbinding flips emptiness, so map
// every tunnel event back to its assigned claim's reconcile.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("exitclaim-emptiness").
		For(&v1alpha1.ExitClaim{}).
		Watches(&v1alpha1.Tunnel{}, handler.EnqueueRequestsFromMapFunc(tunnelToClaim)).
		Complete(r)
}

func tunnelToClaim(_ context.Context, obj client.Object) []reconcile.Request {
	t, ok := obj.(*v1alpha1.Tunnel)
	if !ok {
		return nil
	}
	if t.Status.AssignedExit == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: t.Status.AssignedExit}}}
}
