/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
	"github.com/mtaku3/frp-operator/internal/scheduler"
)

// TunnelReconciler reconciles Tunnel CRs.
type TunnelReconciler struct {
	client.Client
	// APIReader bypasses the controller-runtime informer cache when
	// listing ExitServers in allocateExit. The cache can lag behind
	// apiserver across the small window between an ExitServer
	// reaching Phase=Ready and a freshly-created Tunnel reconciling
	// — without a direct read the scheduler sees no eligible exits
	// and provisions a duplicate instead of binpacking.
	APIReader client.Reader
	Scheme    *runtime.Scheme

	Allocators          *scheduler.AllocatorRegistry
	ProvisionStrategies *scheduler.ProvisionStrategyRegistry
	Provisioners        *provider.Registry
	NewAdminClient      AdminClientFactory
}

// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels/finalizers,verbs=update
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=schedulingpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives one Tunnel toward its desired state.
func (r *TunnelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var tunnel frpv1alpha1.Tunnel
	if err := r.Get(ctx, req.NamespacedName, &tunnel); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	tunnelKey := tunnel.Namespace + "/" + tunnel.Name

	if !tunnel.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tunnel)
	}

	if added, err := addTunnelFinalizer(ctx, r.Client, &tunnel); err != nil {
		return ctrl.Result{}, err
	} else if added {
		return ctrl.Result{Requeue: true}, nil
	}

	var exit *frpv1alpha1.ExitServer
	if tunnel.Status.AssignedExit == "" {
		var err error
		var justProvisioned bool
		exit, justProvisioned, err = r.allocateExit(ctx, &tunnel)
		if err != nil {
			return ctrl.Result{}, err
		}
		if exit == nil {
			return r.patchTunnelPhase(ctx, &tunnel, frpv1alpha1.TunnelAllocating, "no eligible exit; pending")
		}
		if justProvisioned {
			// Newly-created exit: record it and wait for Ready.
			return r.patchTunnelPhaseWithExit(ctx, &tunnel, exit, frpv1alpha1.TunnelProvisioning, "provisioning new exit")
		}
	} else {
		var got frpv1alpha1.ExitServer
		err := r.Get(ctx, types.NamespacedName{Name: tunnel.Status.AssignedExit, Namespace: tunnel.Namespace}, &got)
		if apierrors.IsNotFound(err) {
			// Assigned exit vanished (reclaimed, manually deleted, etc).
			// Clear AssignedExit and let the next pass re-schedule.
			patch := client.MergeFrom(tunnel.DeepCopy())
			tunnel.Status.AssignedExit = ""
			if perr := r.Status().Patch(ctx, &tunnel, patch); perr != nil {
				return ctrl.Result{}, fmt.Errorf("clear stale assignedExit: %w", perr)
			}
			return ctrl.Result{Requeue: true}, nil
		}
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("refetch assigned exit: %w", err)
		}
		exit = &got
	}

	if exit.Status.Phase != frpv1alpha1.PhaseReady {
		return r.patchTunnelPhaseWithExit(ctx, &tunnel, exit, frpv1alpha1.TunnelProvisioning, "exit not yet Ready")
	}

	publicPorts := tunnelPublicPorts(&tunnel)
	if err := reservePorts(ctx, r.Client, exit, publicPorts, tunnelKey); err != nil {
		logger.V(1).Info("port reservation failed; will requeue", "err", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	var credSec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: credentialsSecretName(exit), Namespace: exit.Namespace}, &credSec); err != nil {
		return ctrl.Result{}, fmt.Errorf("get exit credentials: %w", err)
	}
	authToken := string(credSec.Data["auth-token"])

	bindPort := int(exit.Spec.Frps.BindPort)
	if bindPort == 0 {
		bindPort = 7000
	}
	if _, err := ensureFrpcSecret(ctx, r.Client, &tunnel, exit.Status.PublicIP, bindPort, authToken, tunnel.Spec.Ports); err != nil {
		return ctrl.Result{}, err
	}
	if err := ensureFrpcDeployment(ctx, r.Client, &tunnel); err != nil {
		return ctrl.Result{}, err
	}

	frpcReady, err := isFrpcDeploymentReady(ctx, r.Client, &tunnel)
	if err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(tunnel.DeepCopy())
	tunnel.Status.AssignedExit = exit.Name
	tunnel.Status.AssignedIP = exit.Status.PublicIP
	tunnel.Status.AssignedPorts = publicPorts
	tunnel.Status.Phase = nextTunnelPhase(tunnel.Status.Phase, true, true, frpcReady)
	apimeta.SetStatusCondition(&tunnel.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus(frpcReady),
		ObservedGeneration: tunnel.Generation,
		Reason:             "Reconciled",
	})
	if err := r.Status().Patch(ctx, &tunnel, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch tunnel status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// reconcileDelete: release ports, then drop finalizer.
func (r *TunnelReconciler) reconcileDelete(ctx context.Context, tunnel *frpv1alpha1.Tunnel) (ctrl.Result, error) {
	if !hasTunnelFinalizer(tunnel) {
		return ctrl.Result{}, nil
	}
	tunnelKey := tunnel.Namespace + "/" + tunnel.Name

	if tunnel.Status.AssignedExit != "" {
		var exit frpv1alpha1.ExitServer
		err := r.Get(ctx, types.NamespacedName{Name: tunnel.Status.AssignedExit, Namespace: tunnel.Namespace}, &exit)
		switch {
		case err == nil:
			if err := releasePorts(ctx, r.Client, &exit, tunnelKey); err != nil {
				return ctrl.Result{}, err
			}
		case apierrors.IsNotFound(err):
		default:
			return ctrl.Result{}, err
		}
	}

	if _, err := removeTunnelFinalizer(ctx, r.Client, tunnel); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// allocateExit runs the scheduler. Returns nil exit if a new ExitServer is
// being provisioned or no eligible exit exists. justProvisioned indicates
// whether a new ExitServer was just created.
func (r *TunnelReconciler) allocateExit(ctx context.Context, tunnel *frpv1alpha1.Tunnel) (*frpv1alpha1.ExitServer, bool, error) {
	if tunnel.Spec.ExitRef != nil {
		var got frpv1alpha1.ExitServer
		if err := r.Get(ctx, types.NamespacedName{Name: tunnel.Spec.ExitRef.Name, Namespace: tunnel.Namespace}, &got); err != nil {
			return nil, false, fmt.Errorf("hard-pinned ExitRef: %w", err)
		}
		return &got, false, nil
	}

	policy, err := resolvePolicy(ctx, r.Client, tunnel)
	if err != nil {
		return nil, false, err
	}
	reader := client.Reader(r.Client)
	if r.APIReader != nil {
		reader = r.APIReader
	}
	exits, err := listExitsInScope(ctx, reader, tunnel)
	if err != nil {
		return nil, false, err
	}

	// Idempotency: if a prior reconcile already created an exit for this
	// tunnel (label created-for-tunnel=<name>), reuse it instead of
	// running the allocator/provisioner again. Without this guard, a
	// status patch failure between Create and AssignedExit-set would
	// re-enter allocateExit with AssignedExit="" and provision a
	// duplicate exit on every retry.
	for i := range exits {
		e := &exits[i]
		if e.Labels["frp-operator.io/created-for-tunnel"] == tunnel.Name {
			return e, false, nil
		}
	}

	allocName := string(policy.Spec.Allocator)
	if allocName == "" {
		allocName = "CapacityAware"
	}
	alloc, err := r.Allocators.Lookup(allocName)
	if err != nil {
		return nil, false, fmt.Errorf("allocator %q: %w", allocName, err)
	}
	decision, err := alloc.Allocate(scheduler.AllocateInput{Tunnel: tunnel, Exits: exits})
	if err != nil {
		return nil, false, fmt.Errorf("allocate: %w", err)
	}
	if decision.Exit != nil {
		return decision.Exit, false, nil
	}
	// Allocator returned no exit. Log the candidate set so binpack
	// regressions are visible in operator logs without instrumentation.
	{
		logger := log.FromContext(ctx)
		summary := make([]string, 0, len(exits))
		for i := range exits {
			e := &exits[i]
			summary = append(summary, fmt.Sprintf(
				"%s[phase=%s,allowPorts=%v,allocations=%v]",
				e.Name, e.Status.Phase, e.Spec.AllowPorts, e.Status.Allocations))
		}
		logger.Info("scheduler: no eligible exit, will provision",
			"reason", decision.Reason,
			"tunnelPorts", tunnelPublicPorts(tunnel),
			"candidates", summary)
	}

	provName := string(policy.Spec.Provisioner)
	if provName == "" {
		provName = "OnDemand"
	}
	ps, err := r.ProvisionStrategies.Lookup(provName)
	if err != nil {
		return nil, false, fmt.Errorf("provision strategy %q: %w", provName, err)
	}
	pd, err := ps.Plan(scheduler.ProvisionInput{Tunnel: tunnel, Policy: policy, Current: exits})
	if err != nil {
		return nil, false, fmt.Errorf("plan: %w", err)
	}
	if !pd.Provision {
		return nil, false, nil
	}
	newExit, err := createExitServerFromDecision(ctx, r.Client, tunnel, pd)
	if err != nil {
		return nil, false, err
	}
	return newExit, true, nil
}

// patchTunnelPhase patches just status.phase when the tunnel is not yet placed.
func (r *TunnelReconciler) patchTunnelPhase(ctx context.Context, t *frpv1alpha1.Tunnel, phase frpv1alpha1.TunnelPhase, reason string) (ctrl.Result, error) {
	patch := client.MergeFrom(t.DeepCopy())
	t.Status.Phase = phase
	apimeta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: t.Generation,
		Reason:             "NotReady",
		Message:            reason,
	})
	if err := r.Status().Patch(ctx, t, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// patchTunnelPhaseWithExit patches phase and assignedExit when the tunnel has
// an exit but it isn't Ready yet.
func (r *TunnelReconciler) patchTunnelPhaseWithExit(ctx context.Context, t *frpv1alpha1.Tunnel, exit *frpv1alpha1.ExitServer, phase frpv1alpha1.TunnelPhase, reason string) (ctrl.Result, error) {
	patch := client.MergeFrom(t.DeepCopy())
	t.Status.AssignedExit = exit.Name
	t.Status.Phase = phase
	apimeta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: t.Generation,
		Reason:             "NotReady",
		Message:            reason,
	})
	if err := r.Status().Patch(ctx, t, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func tunnelPublicPorts(t *frpv1alpha1.Tunnel) []int32 {
	out := make([]int32, 0, len(t.Spec.Ports))
	for _, p := range t.Spec.Ports {
		if p.PublicPort != nil {
			out = append(out, *p.PublicPort)
		} else {
			out = append(out, p.ServicePort)
		}
	}
	return out
}

func condStatus(b bool) metav1.ConditionStatus {
	if b {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

// isFrpcDeploymentReady returns true if the frpc Deployment for the tunnel
// has at least one ready replica. envtest doesn't run kubelets so this
// will always be false there.
func isFrpcDeploymentReady(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (bool, error) {
	var dep appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: frpcDeploymentName(t), Namespace: t.Namespace}, &dep)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return dep.Status.ReadyReplicas >= 1, nil
}

// SetupWithManager wires the controller.
func (r *TunnelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&frpv1alpha1.Tunnel{}).
		Owns(&corev1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Named("tunnel").
		Complete(r)
}
