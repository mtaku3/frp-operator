package disruption

import (
	"context"
	"math"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// PollInterval is the cadence of the disruption loop. Mirrors Karpenter's
// 10s default.
const PollInterval = 10 * time.Second

// Controller is the disruption singleton. It polls every PollInterval,
// builds candidates, asks each Method (in priority order) for commands, and
// hands the first method's commands to the Queue. The Karpenter convention
// is to stop after the first method that produced commands and re-evaluate
// next loop — this keeps the control loop simple and fair across methods.
type Controller struct {
	Cluster    *state.Cluster
	KubeClient client.Client
	Queue      *Queue
	Methods    []Method
	Now        func() time.Time
}

// New constructs a Controller wired with the given Methods. The list is the
// priority order — first match wins.
func New(c *state.Cluster, kube client.Client, q *Queue, methods []Method) *Controller {
	return &Controller{
		Cluster:    c,
		KubeClient: kube,
		Queue:      q,
		Methods:    methods,
		Now:        time.Now,
	}
}

// Reconcile is one disruption pass. Safe to call from a ticker-driven
// runnable. Returns a result with RequeueAfter set to the next desired tick.
func (r *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	logger := log.FromContext(ctx).WithName("disruption")
	if !r.Cluster.Synced(ctx) {
		return reconcile.Result{RequeueAfter: time.Second}, nil
	}

	poolByName := r.poolLookup()
	candidates := GetCandidates(r.Cluster, poolByName)
	if len(candidates) == 0 {
		return reconcile.Result{RequeueAfter: PollInterval}, nil
	}

	for _, m := range r.Methods {
		eligible := []*Candidate{}
		for _, c := range candidates {
			if m.ShouldDisrupt(ctx, c) {
				eligible = append(eligible, c)
			}
		}
		if len(eligible) == 0 {
			continue
		}

		budgets := r.computeBudgets(eligible, m)
		commands, err := m.ComputeCommands(ctx, budgets, eligible...)
		if err != nil {
			logger.Error(err, "compute commands", "method", m.Name())
			continue
		}
		if len(commands) == 0 {
			continue
		}

		anyExecuted := false
		for _, cmd := range commands {
			if err := Validate(ctx, r.Cluster, cmd); err != nil {
				logger.V(1).Info("validation rejected command", "method", m.Name(), "err", err.Error())
				continue
			}
			if err := r.Queue.Enqueue(ctx, cmd); err != nil {
				logger.Error(err, "enqueue", "method", m.Name())
				return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
			}
			anyExecuted = true
		}
		if anyExecuted {
			// Karpenter pattern: stop after the first method that fired.
			return reconcile.Result{RequeueAfter: PollInterval}, nil
		}
	}
	return reconcile.Result{RequeueAfter: PollInterval}, nil
}

// computeBudgets builds the BudgetMap for the given method. Forceful methods
// receive a synthetic max-int budget for every relevant pool so they cannot
// be gated.
func (r *Controller) computeBudgets(candidates []*Candidate, m Method) BudgetMap {
	budgets := BudgetMap{}
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if c == nil || c.Pool == nil {
			continue
		}
		if _, ok := seen[c.Pool.Name]; ok {
			continue
		}
		seen[c.Pool.Name] = struct{}{}
		if m.Forceful() {
			budgets.Set(c.Pool.Name, m.Reason(), math.MaxInt32)
			continue
		}
		allowed := GetAllowedDisruptionsByReason(r.Cluster, c.Pool, m.Reason(), r.now())
		budgets.Set(c.Pool.Name, m.Reason(), allowed)
	}
	return budgets
}

// poolLookup snapshots pool data once per reconcile so each Candidate sees a
// consistent ExitPool view.
func (r *Controller) poolLookup() PoolLookup {
	pools := map[string]*v1alpha1.ExitPool{}
	for _, sp := range r.Cluster.Pools() {
		snap := sp.Snapshot()
		if snap == nil {
			continue
		}
		pools[snap.Name] = snap
	}
	return func(name string) *v1alpha1.ExitPool {
		return pools[name]
	}
}

// SetupWithManager registers a Runnable that drives Reconcile on PollInterval.
func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		ticker := time.NewTicker(PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if _, err := r.Reconcile(ctx); err != nil {
					log.FromContext(ctx).Error(err, "disruption reconcile")
				}
			}
		}
	}))
}

func (r *Controller) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
