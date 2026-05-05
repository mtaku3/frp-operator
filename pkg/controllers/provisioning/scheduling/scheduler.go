package scheduling

import (
	"context"
	"fmt"
	"sort"
	"time"

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
	salt           string
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
	s.salt = fmt.Sprintf("%d", time.Now().UnixNano())
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
	results.NewClaims = s.inflightClaims
	return results, nil
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
		c := NewClaimFromPool(pool, s.salt+"|"+tunnelKey(t))
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
// match or exceed any configured Limit. Reads c.Pool(name).Resources.
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
	used := sp.Resources
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
