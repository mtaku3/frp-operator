package provisioning

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/controllers/provisioning/scheduling"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Provisioner is the singleton reconciler that translates batched
// Tunnel events into ExitClaim Creates and Tunnel.Status patches.
type Provisioner struct {
	Cluster       *state.Cluster
	KubeClient    client.Client
	CloudProvider *cloudprovider.Registry
	Batcher       *Batcher[types.UID]
	Scheduler     *scheduling.Scheduler
}

// New constructs a Provisioner with default batch windows.
func New(c *state.Cluster, kube client.Client, cp *cloudprovider.Registry) *Provisioner {
	b := NewBatcher[types.UID](DefaultBatchIdleDuration, DefaultBatchMaxDuration)
	return &Provisioner{
		Cluster:       c,
		KubeClient:    kube,
		CloudProvider: cp,
		Batcher:       b,
		Scheduler:     scheduling.New(c, cp, kube),
	}
}

// Trigger is exposed publicly so Phase 9 wiring can plug it as the
// cluster cache's provisioner trigger.
func (p *Provisioner) Trigger(uid types.UID) { p.Batcher.Trigger(uid) }

// Reconcile is invoked in a loop by the manager runnable.
func (p *Provisioner) Reconcile(ctx context.Context) (reconcile.Result, error) {
	if !p.Batcher.Wait(ctx) {
		return reconcile.Result{}, nil
	}
	if !p.Cluster.Synced(ctx) {
		return reconcile.Result{RequeueAfter: 1 * time.Second}, nil
	}
	// Drain to clear the pending set; the actual work uses a fresh List.
	_ = p.Batcher.Drain()

	pending, err := p.listPendingTunnels(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	if len(pending) == 0 {
		return reconcile.Result{}, nil
	}

	results, err := p.Scheduler.Solve(ctx, pending)
	if err != nil {
		return reconcile.Result{}, err
	}
	if err := p.persistResults(ctx, results); err != nil {
		return reconcile.Result{}, err
	}
	log.FromContext(ctx).Info("provisioning solve complete",
		"tunnels", len(pending),
		"newClaims", len(results.NewClaims),
		"bindings", len(results.Bindings),
		"errors", len(results.TunnelErrors))
	return reconcile.Result{}, nil
}

// SetupWithManager registers the Provisioner as a Runnable that loops
// invoking Reconcile. controller-runtime doesn't natively support a
// no-Request reconciler, so we drive it directly.
func (p *Provisioner) SetupWithManager(mgr ctrl.Manager) error {
	return mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		for {
			if ctx.Err() != nil {
				return nil
			}
			res, err := p.Reconcile(ctx)
			if err != nil {
				log.FromContext(ctx).Error(err, "provisioner reconcile")
			}
			if res.RequeueAfter > 0 {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(res.RequeueAfter):
				}
			}
		}
	}))
}

func (p *Provisioner) listPendingTunnels(ctx context.Context) ([]*v1alpha1.Tunnel, error) {
	var list v1alpha1.TunnelList
	if err := p.KubeClient.List(ctx, &list); err != nil {
		return nil, err
	}
	out := []*v1alpha1.Tunnel{}
	for i := range list.Items {
		t := &list.Items[i]
		if t.DeletionTimestamp != nil {
			continue
		}
		if t.Status.AssignedExit == "" || t.Status.Phase == v1alpha1.TunnelPhaseAllocating {
			out = append(out, t)
		}
	}
	return out, nil
}

func (p *Provisioner) persistResults(ctx context.Context, r scheduling.Results) error {
	// 1. Create each NewClaim. Idempotent on AlreadyExists.
	for _, c := range r.NewClaims {
		labels := map[string]string{}
		if c.Pool != nil {
			labels[v1alpha1.LabelExitPool] = c.Pool.Name
		}
		ec := &v1alpha1.ExitClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   c.Name,
				Labels: labels,
			},
			Spec: c.Spec,
		}
		if err := p.KubeClient.Create(ctx, ec); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create ExitClaim %s: %w", c.Name, err)
		}
	}
	// 2. Patch tunnel status for each binding.
	for _, b := range r.Bindings {
		ns, name := splitKey(b.TunnelKey)
		var t v1alpha1.Tunnel
		if err := p.KubeClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &t); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("get tunnel %s: %w", b.TunnelKey, err)
		}
		patch := client.MergeFrom(t.DeepCopy())
		t.Status.AssignedExit = b.ExitClaimName
		t.Status.AssignedPorts = append([]int32(nil), b.AssignedPorts...)
		t.Status.Phase = v1alpha1.TunnelPhaseProvisioning
		setTunnelCondition(&t, "Ready", metav1.ConditionFalse, "Provisioning", "scheduler bound to "+b.ExitClaimName)
		if err := p.KubeClient.Status().Patch(ctx, &t, patch); err != nil {
			return fmt.Errorf("patch tunnel %s: %w", b.TunnelKey, err)
		}
	}
	// 3. Surface errors on tunnels that couldn't be scheduled.
	for key, perr := range r.TunnelErrors {
		ns, name := splitKey(key)
		var t v1alpha1.Tunnel
		if err := p.KubeClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &t); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("get tunnel %s: %w", key, err)
		}
		patch := client.MergeFrom(t.DeepCopy())
		if t.Status.Phase == "" || t.Status.Phase == v1alpha1.TunnelPhasePending {
			t.Status.Phase = v1alpha1.TunnelPhaseAllocating
		}
		setTunnelCondition(&t, "Ready", metav1.ConditionFalse, "Unschedulable", perr.Error())
		if err := p.KubeClient.Status().Patch(ctx, &t, patch); err != nil {
			return fmt.Errorf("patch tunnel %s: %w", key, err)
		}
	}
	return nil
}

func setTunnelCondition(t *v1alpha1.Tunnel, condType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	for i := range t.Status.Conditions {
		c := &t.Status.Conditions[i]
		if c.Type != condType {
			continue
		}
		if c.Status != status {
			c.LastTransitionTime = now
		}
		c.Status = status
		c.Reason = reason
		c.Message = message
		c.ObservedGeneration = t.Generation
		return
	}
	t.Status.Conditions = append(t.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		ObservedGeneration: t.Generation,
	})
}

func splitKey(key string) (string, string) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}
