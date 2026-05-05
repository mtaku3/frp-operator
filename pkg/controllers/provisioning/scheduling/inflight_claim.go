package scheduling

import (
	"fmt"
	"sort"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// InflightClaim is a not-yet-persisted ExitClaim being assembled inside
// one Solve run. Subsequent tunnels in the same Solve can pack onto it
// via addToInflightClaim.
type InflightClaim struct {
	Spec      v1alpha1.ExitClaimSpec
	Name      string
	Pool      *v1alpha1.ExitPool
	Tunnels   []*v1alpha1.Tunnel
	UsedPorts map[int32]struct{}
}

// CanAdd checks the same three predicates as ExistingExit but against
// the pool template's declared port pool. Capacity binpacking is a
// Phase 7 refinement; for now we treat capacity as unbounded (the pool
// template carries no Allocatable yet — that's filled in by
// cloudProvider.Create at lifecycle time).
func (c *InflightClaim) CanAdd(tunnel *v1alpha1.Tunnel) ([]int32, error) {
	if err := Compatible(c.Spec.Requirements, tunnel.Spec.Requirements); err != nil {
		return nil, err
	}
	assigned, ok := ResolveAutoAssign(c.Spec.Frps.AllowPorts, c.Spec.Frps.ReservedPorts, c.UsedPorts, tunnel.Spec.Ports)
	if !ok {
		return nil, fmt.Errorf("ports don't fit on inflight claim %s", c.Name)
	}
	return assigned, nil
}

// Add records the binding on this in-flight claim.
func (c *InflightClaim) Add(tunnel *v1alpha1.Tunnel, assignedPorts []int32) {
	c.Tunnels = append(c.Tunnels, tunnel)
	if c.UsedPorts == nil {
		c.UsedPorts = map[int32]struct{}{}
	}
	for _, p := range assignedPorts {
		c.UsedPorts[p] = struct{}{}
	}
}

// SortByLoad sorts the input so least-loaded inflight claims come first.
// Mirrors Karpenter's nodeclaim sort: pack tighter onto already-populated
// claims, but cycle through to spread when capacity is similar.
func SortByLoad(claims []*InflightClaim) {
	sort.SliceStable(claims, func(i, j int) bool {
		return len(claims[i].Tunnels) < len(claims[j].Tunnels)
	})
}
