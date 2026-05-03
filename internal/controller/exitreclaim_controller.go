package controller

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// ExitReclaimReconciler watches ExitServer CRs and applies the empty-exit
// reclamation policy. It does NOT own VPS lifecycle — deletion of the CR
// triggers the ExitServerController's finalizer, which calls
// Provisioner.Destroy.
type ExitReclaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// PolicyName is the cluster-scoped SchedulingPolicy whose consolidation
	// settings drive reclaim. Defaults to "default".
	PolicyName string

	// Now is overridable for tests. Defaults to time.Now.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=schedulingpolicies,verbs=get;list;watch

// Reconcile implements the reclamation state machine via decideReclaim.
func (r *ExitReclaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var exit frpv1alpha1.ExitServer
	if err := r.Get(ctx, req.NamespacedName, &exit); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// Don't intervene on objects already being deleted.
	if !exit.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	enabled, drainAfter, err := r.resolveConsolidation(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	now := r.now()
	action := decideReclaim(&exit, enabled, drainAfter, now)
	logger.V(1).Info("reclaim decision", "exit", exit.Name, "phase", exit.Status.Phase, "action", action)

	switch action {
	case reclaimActionNoOp:
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	case reclaimActionStartDrain:
		patch := client.MergeFrom(exit.DeepCopy())
		ts := metav1.NewTime(now)
		exit.Status.Phase = frpv1alpha1.PhaseDraining
		exit.Status.DrainStartedAt = &ts
		if err := r.Status().Patch(ctx, &exit, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("start drain: %w", err)
		}
		return ctrl.Result{RequeueAfter: drainAfter}, nil
	case reclaimActionAbortDrain:
		patch := client.MergeFrom(exit.DeepCopy())
		exit.Status.Phase = frpv1alpha1.PhaseReady
		exit.Status.DrainStartedAt = nil
		if err := r.Status().Patch(ctx, &exit, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("abort drain: %w", err)
		}
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	case reclaimActionRequeueDrain:
		var remaining time.Duration
		if exit.Status.DrainStartedAt != nil {
			elapsed := now.Sub(exit.Status.DrainStartedAt.Time)
			remaining = max(drainAfter-elapsed, time.Second)
		} else {
			remaining = drainAfter
		}
		return ctrl.Result{RequeueAfter: remaining}, nil
	case reclaimActionDestroy:
		if err := r.Delete(ctx, &exit); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete exit: %w", err)
		}
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

// resolveConsolidation looks up the SchedulingPolicy named PolicyName and
// returns its reclaim flag + drainAfter. If the policy is missing, defaults
// to enabled=true with drainAfter=10m (matches the spec defaults).
func (r *ExitReclaimReconciler) resolveConsolidation(ctx context.Context) (bool, time.Duration, error) {
	name := r.PolicyName
	if name == "" {
		name = "default"
	}
	var p frpv1alpha1.SchedulingPolicy
	err := r.Get(ctx, types.NamespacedName{Name: name}, &p)
	if apierrors.IsNotFound(err) {
		return true, 10 * time.Minute, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("get SchedulingPolicy %q: %w", name, err)
	}
	drain := p.Spec.Consolidation.DrainAfter.Duration
	if drain == 0 {
		drain = 10 * time.Minute
	}
	// ReclaimEmpty's default is true (defined in the CRD), so we read
	// it as-is.
	return p.Spec.Consolidation.ReclaimEmpty, drain, nil
}

// SetupWithManager wires the controller. Watches ExitServer CRs only.
// Tunnel changes trigger ExitServer status updates (port allocations),
// which trigger this controller as a side effect.
func (r *ExitReclaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&frpv1alpha1.ExitServer{}).
		Named("exitreclaim").
		Complete(r)
}

// now returns the configured clock or wall-clock time.
func (r *ExitReclaimReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
