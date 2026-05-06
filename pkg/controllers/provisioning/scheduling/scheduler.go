package scheduling

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Scheduler runs Solve over a batch of pending tunnels.
type Scheduler struct {
	Cluster       *state.Cluster
	CloudProvider *cloudprovider.Registry
	KubeClient    client.Client
	Preferences   *Preferences

	// mutable per-Solve:
	existingExits  []*ExistingExit
	inflightClaims []*InflightClaim
	pools          []*v1alpha1.ExitPool
}

// New constructs a Scheduler. Cluster + Registry are required; KubeClient
// may be nil for pure-unit tests that don't touch the apiserver.
func New(c *state.Cluster, cp *cloudprovider.Registry, kube client.Client) *Scheduler {
	return &Scheduler{
		Cluster:       c,
		CloudProvider: cp,
		KubeClient:    kube,
		Preferences:   &Preferences{Policy: "Respect"},
	}
}

// Solve produces a Results from a list of tunnels.
func (s *Scheduler) Solve(ctx context.Context, tunnels []*v1alpha1.Tunnel) (Results, error) {
	s.existingExits = nil
	s.inflightClaims = nil
	s.pools = nil

	for _, se := range s.Cluster.Exits() {
		s.existingExits = append(s.existingExits, &ExistingExit{State: se})
	}
	for _, sp := range s.Cluster.Pools() {
		snap := sp.Snapshot()
		if snap == nil {
			continue
		}
		s.pools = append(s.pools, snap)
	}
	sortPoolsByWeight(s.pools)

	// Rehydrate inflight from persisted but non-Ready claims so subsequent
	// Solves still see in-progress claims for binpacking. Mirrors how
	// Karpenter retains scheduled-but-not-Ready NodeClaims across batches.
	// Without this, a Solve that runs before the previous Solve's claim
	// reaches Ready would mint a fresh duplicate claim instead of packing
	// onto the pending one.
	rehydrated := map[string]struct{}{}
	rehydrate := func(claim *v1alpha1.ExitClaim, used map[int32]struct{}) {
		if _, dup := rehydrated[claim.Name]; dup {
			return
		}
		poolName := claim.Labels[v1alpha1.LabelExitPool]
		var pool *v1alpha1.ExitPool
		for _, p := range s.pools {
			if p.Name == poolName {
				pool = p
				break
			}
		}
		if pool == nil {
			return
		}
		s.inflightClaims = append(s.inflightClaims, &InflightClaim{
			Spec:      claim.Spec,
			Name:      claim.Name,
			Pool:      pool,
			UsedPorts: used,
			Persisted: true,
		})
		rehydrated[claim.Name] = struct{}{}
	}
	for _, se := range s.Cluster.Exits() {
		claim, allocs := se.SnapshotForRead()
		if claim == nil {
			continue
		}
		if isReady(claim) {
			continue // existing-exit stage handles Ready ones.
		}
		// Skip claims being torn down: rehydrating them lets the
		// scheduler rebind tunnels onto a doomed claim, fighting
		// finalize and stalling container teardown (issue #8). Same
		// guard exists on the existing-exit path; rehydration needed
		// it too because Cluster.MarkExitForDeletion is also checked
		// independently below.
		if claim.GetDeletionTimestamp() != nil {
			continue
		}
		if se.IsMarkedForDeletion() {
			continue
		}
		used := make(map[int32]struct{}, len(allocs))
		for p := range allocs {
			used[p] = struct{}{}
		}
		rehydrate(claim, used)
	}
	// Pending claims (Status.ProviderID still empty) live in a separate
	// index because they have no StateExit yet. Without rehydrating them
	// the scheduler can mint a duplicate ExitClaim across separate Solve
	// runs (issue #7).
	for _, claim := range s.Cluster.PendingClaims() {
		if isReady(claim) {
			continue
		}
		if claim.GetDeletionTimestamp() != nil {
			continue // see issue #8.
		}
		rehydrate(claim, s.Cluster.PortsForClaimName(claim.Name))
	}

	results := Results{TunnelErrors: map[string]error{}}
	for _, t := range tunnels {
		if err := s.add(ctx, t, &results); err != nil {
			if !s.Preferences.Relax(t) {
				results.TunnelErrors[tunnelKey(t)] = err
				continue
			}
			if err2 := s.add(ctx, t, &results); err2 != nil {
				results.TunnelErrors[tunnelKey(t)] = err2
			}
		}
	}
	for _, c := range s.inflightClaims {
		if c.Persisted {
			continue
		}
		results.NewClaims = append(results.NewClaims, c)
	}
	return results, nil
}

// isReady reports whether the claim has a Ready=True condition.
func isReady(claim *v1alpha1.ExitClaim) bool {
	for _, c := range claim.Status.Conditions {
		if c.Type == v1alpha1.ConditionTypeReady && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

func (s *Scheduler) add(_ context.Context, t *v1alpha1.Tunnel, r *Results) error {
	if err := s.addToExistingExit(t, r); err == nil {
		return nil
	}
	if err := s.addToInflightClaim(t, r); err == nil {
		return nil
	}
	return s.addToNewClaim(t, r)
}

func (s *Scheduler) addToExistingExit(t *v1alpha1.Tunnel, r *Results) error {
	for _, e := range s.existingExits {
		assigned, err := e.CanAdd(t)
		if err != nil {
			continue
		}
		e.Add(t, assigned)
		r.Bindings = append(r.Bindings, Binding{
			TunnelKey:     tunnelKey(t),
			ExitClaimName: e.State.Claim.Name,
			AssignedPorts: assigned,
		})
		return nil
	}
	return fmt.Errorf("no existing exit fits")
}

func (s *Scheduler) addToInflightClaim(t *v1alpha1.Tunnel, r *Results) error {
	SortByLoad(s.inflightClaims)
	for _, c := range s.inflightClaims {
		assigned, err := c.CanAdd(t)
		if err != nil {
			continue
		}
		c.Add(t, assigned)
		r.Bindings = append(r.Bindings, Binding{
			TunnelKey:     tunnelKey(t),
			ExitClaimName: c.Name,
			AssignedPorts: assigned,
		})
		return nil
	}
	return fmt.Errorf("no inflight claim fits")
}

func (s *Scheduler) addToNewClaim(t *v1alpha1.Tunnel, r *Results) error {
	var lastErr error
	for _, pool := range s.pools {
		if err := Compatible(pool.Spec.Template.Spec.Requirements, t.Spec.Requirements); err != nil {
			lastErr = err
			continue
		}
		if exceeded, dim := poolLimitsExceeded(pool, s.Cluster); exceeded {
			lastErr = fmt.Errorf("pool %q limit %s exceeded", pool.Name, dim)
			continue
		}
		c := NewClaimFromPool(pool, string(t.UID))
		assigned, err := c.CanAdd(t)
		if err != nil {
			lastErr = err
			continue
		}
		c.Add(t, assigned)
		s.inflightClaims = append(s.inflightClaims, c)
		r.Bindings = append(r.Bindings, Binding{
			TunnelKey:     tunnelKey(t),
			ExitClaimName: c.Name,
			AssignedPorts: assigned,
		})
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no pool can produce a claim for tunnel %s", tunnelKey(t))
}

// sortPoolsByWeight orders pools highest-weight-first. Karpenter does the
// same: a pool with weight=100 wins over weight=10. Unset weight = 0.
func sortPoolsByWeight(pools []*v1alpha1.ExitPool) {
	sort.SliceStable(pools, func(i, j int) bool {
		return weight(pools[i]) > weight(pools[j])
	})
}

func weight(p *v1alpha1.ExitPool) int32 {
	if p == nil || p.Spec.Weight == nil {
		return 0
	}
	return *p.Spec.Weight
}

// poolLimitsExceeded reports whether the pool's running totals already
// match or exceed any configured Limit. Reads via Pool.SnapshotResources.
//
// TODO(phase7): the Resources counter is populated by the
// counter-controller in Phase 7. For Phase 4 this is zero-valued, so
// limits never bind.
func poolLimitsExceeded(pool *v1alpha1.ExitPool, c *state.Cluster) (bool, string) {
	if pool == nil || c == nil {
		return false, ""
	}
	limits := pool.Spec.Limits
	if len(limits) == 0 {
		return false, ""
	}
	sp := c.Pool(pool.Name)
	if sp == nil {
		return false, ""
	}
	used, _ := sp.SnapshotResources()
	for k, lim := range limits {
		cur, ok := used[k]
		if !ok {
			continue
		}
		if cur.Cmp(lim) >= 0 {
			return true, string(k)
		}
	}
	return false, ""
}

func tunnelKey(t *v1alpha1.Tunnel) string { return t.Namespace + "/" + t.Name }
