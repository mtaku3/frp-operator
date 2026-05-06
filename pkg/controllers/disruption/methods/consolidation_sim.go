package methods

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/provisioning/scheduling"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Simulator decides whether the tunnels currently bound to a set of
// candidate exits could be re-packed onto the remaining ready exits in
// the cluster. Used by SingleNodeConsolidation and MultiNodeConsolidation.
//
// CloudProvider is optional. When set, CanRepackWithReplacement
// becomes available — it tries fitting moveable tunnels onto a single
// new claim picked from the cheapest valid offering, then compares
// total price against the candidate aggregate.
type Simulator struct {
	Cluster       *state.Cluster
	KubeClient    client.Client
	CloudProvider *cloudprovider.Registry
}

func NewSimulator(c *state.Cluster, kube client.Client, registry *cloudprovider.Registry) *Simulator {
	return &Simulator{Cluster: c, KubeClient: kube, CloudProvider: registry}
}

// CanRepack returns nil if every tunnel currently bound to the candidate
// exits can be re-bound onto a different ready exit in the cluster (existing
// non-candidate exits only — no new claims). Returns an error describing the
// first tunnel that fails to fit.
func (s *Simulator) CanRepack(ctx context.Context, candidates []*disruption.Candidate) error {
	if s == nil || s.Cluster == nil || s.KubeClient == nil {
		return fmt.Errorf("simulator missing dependencies")
	}
	candidateNames := map[string]struct{}{}
	for _, c := range candidates {
		if c == nil || c.Claim == nil {
			continue
		}
		candidateNames[c.Claim.Name] = struct{}{}
	}
	if len(candidateNames) == 0 {
		return nil
	}

	// Build per-Solve ExistingExit wrappers for every non-candidate exit.
	pool := []*scheduling.ExistingExit{}
	for _, se := range s.Cluster.Exits() {
		claim, _ := se.SnapshotForRead()
		if claim == nil {
			continue
		}
		if _, isCandidate := candidateNames[claim.Name]; isCandidate {
			continue
		}
		if se.IsMarkedForDeletion() {
			continue
		}
		pool = append(pool, &scheduling.ExistingExit{State: se})
	}

	// Collect the set of tunnels currently bound to the candidates.
	var tunnelList v1alpha1.TunnelList
	if err := s.KubeClient.List(ctx, &tunnelList); err != nil {
		return fmt.Errorf("list tunnels: %w", err)
	}
	moveable := []*v1alpha1.Tunnel{}
	for i := range tunnelList.Items {
		t := &tunnelList.Items[i]
		if t.DeletionTimestamp != nil {
			continue
		}
		if _, ok := candidateNames[t.Status.AssignedExit]; ok {
			moveable = append(moveable, t)
		}
	}
	if len(moveable) == 0 {
		// Nothing to repack — trivially "fits".
		return nil
	}

	// Greedy first-fit. Identical to Scheduler.addToExistingExit but without
	// touching the Cluster cache.
	for _, t := range moveable {
		placed := false
		for _, e := range pool {
			assigned, err := e.CanAdd(t)
			if err != nil {
				continue
			}
			e.Add(t, assigned)
			placed = true
			break
		}
		if !placed {
			return fmt.Errorf("tunnel %s/%s cannot be re-packed onto remaining exits", t.Namespace, t.Name)
		}
	}
	return nil
}

// RepackPlan describes a "replace N with 1" consolidation: drain
// every candidate, launch a single replacement claim, re-pack the
// moveable tunnels onto it. Returned by CanRepackWithReplacement when
// the move is feasible and the replacement is strictly cheaper than
// the candidate aggregate.
type RepackPlan struct {
	Replacement     *v1alpha1.ExitClaim
	CandidateCost   float64
	ReplacementCost float64
}

// CanRepackWithReplacement explores the karpenter "underutilized →
// replace with cheaper instance type" branch. Picks the cheapest
// available offering for the candidates' shared pool that fits the
// moveable tunnel set, builds a replacement claim spec, and reports
// it only if its price is strictly lower than the sum of candidate
// prices. Returns (nil, nil) when the cheaper-replacement path is
// not available (e.g. no Registry, mixed pools, no cheaper offering).
//
// Caller is responsible for ensuring the candidates all share a pool
// — multi-pool consolidation isn't supported in v1alpha1.
func (s *Simulator) CanRepackWithReplacement(
	ctx context.Context, pool *v1alpha1.ExitPool, candidates []*disruption.Candidate,
) (*RepackPlan, error) {
	if s == nil || s.Cluster == nil || s.KubeClient == nil {
		return nil, fmt.Errorf("simulator missing dependencies")
	}
	if s.CloudProvider == nil || pool == nil {
		return nil, nil
	}
	if len(candidates) < 2 {
		return nil, nil
	}

	// Aggregate candidate cost from each claim's pinned instance-type.
	candidateNames := map[string]struct{}{}
	for _, c := range candidates {
		if c == nil || c.Claim == nil {
			continue
		}
		candidateNames[c.Claim.Name] = struct{}{}
	}
	cp, err := s.CloudProvider.For(pool.Spec.Template.Spec.ProviderClassRef.Kind)
	if err != nil {
		return nil, nil
	}
	catalog, err := cp.GetInstanceTypes(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("get instance types: %w", err)
	}
	if len(catalog) == 0 {
		return nil, nil
	}

	candidateCost := 0.0
	for _, c := range candidates {
		if c == nil || c.Claim == nil {
			continue
		}
		p, ok := lookupClaimPrice(catalog, c.Claim)
		if !ok {
			// At least one candidate has no resolvable price; without
			// a complete cost picture we can't claim "cheaper". Bail.
			return nil, nil
		}
		candidateCost += p
	}

	// Collect moveable tunnels.
	var tunnelList v1alpha1.TunnelList
	if err := s.KubeClient.List(ctx, &tunnelList); err != nil {
		return nil, fmt.Errorf("list tunnels: %w", err)
	}
	moveable := []*v1alpha1.Tunnel{}
	for i := range tunnelList.Items {
		t := &tunnelList.Items[i]
		if t.DeletionTimestamp != nil {
			continue
		}
		if _, ok := candidateNames[t.Status.AssignedExit]; ok {
			moveable = append(moveable, t)
		}
	}

	totalRequested := scheduling.Sum(tunnelRequestLists(moveable)...)
	combinedTunnelReqs := combineRequirements(moveable)

	it, off, err := scheduling.SelectInstanceType(
		ctx, s.CloudProvider, pool, combinedTunnelReqs, totalRequested,
	)
	if err != nil {
		return nil, nil // no offering fits — cheaper-replacement infeasible.
	}
	if off.Price >= candidateCost {
		return nil, nil // not strictly cheaper.
	}

	// Build the replacement claim spec. Mirrors scheduling.NewClaimFromPool
	// but tied to the disruption command (no tunnel UID — we'll use the
	// pool name + cost hash as a deterministic name).
	replacement := buildReplacementClaim(pool, it, off, totalRequested)
	if !canPackOntoNewClaim(replacement, moveable) {
		return nil, nil
	}
	return &RepackPlan{
		Replacement:     replacement,
		CandidateCost:   candidateCost,
		ReplacementCost: off.Price,
	}, nil
}

// lookupClaimPrice finds the offering in catalog whose instance-type +
// region match what the claim's Requirements pin. Returns the price
// and ok=true on hit; ok=false when no entry matches (drift, custom
// catalog, ...).
func lookupClaimPrice(
	catalog []*cloudprovider.InstanceType, claim *v1alpha1.ExitClaim,
) (float64, bool) {
	wantSize := pinnedRequirementValue(claim.Spec.Requirements, v1alpha1.RequirementInstanceType)
	wantRegion := pinnedRequirementValue(claim.Spec.Requirements, v1alpha1.RequirementRegion)
	if wantSize == "" {
		return 0, false
	}
	for _, it := range catalog {
		if it.Name != wantSize {
			continue
		}
		for _, off := range it.Offerings {
			if !off.Available {
				continue
			}
			if wantRegion == "" {
				return off.Price, true
			}
			if pinnedRequirementValue(off.Requirements, v1alpha1.RequirementRegion) == wantRegion {
				return off.Price, true
			}
		}
	}
	return 0, false
}

func pinnedRequirementValue(reqs []v1alpha1.NodeSelectorRequirementWithMinValues, key string) string {
	for _, r := range reqs {
		if r.Key != key || r.Operator != v1alpha1.NodeSelectorOpIn {
			continue
		}
		if len(r.Values) == 0 {
			continue
		}
		return r.Values[0]
	}
	return ""
}

func tunnelRequestLists(tunnels []*v1alpha1.Tunnel) []corev1.ResourceList {
	out := make([]corev1.ResourceList, 0, len(tunnels))
	for _, t := range tunnels {
		if t == nil {
			continue
		}
		out = append(out, t.Spec.Resources.Requests)
	}
	return out
}

func combineRequirements(tunnels []*v1alpha1.Tunnel) []v1alpha1.NodeSelectorRequirementWithMinValues {
	seen := map[string]struct{}{}
	out := []v1alpha1.NodeSelectorRequirementWithMinValues{}
	for _, t := range tunnels {
		if t == nil {
			continue
		}
		for _, r := range t.Spec.Requirements {
			key := r.Key + "|" + string(r.Operator)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

func buildReplacementClaim(
	pool *v1alpha1.ExitPool, it *cloudprovider.InstanceType, off *cloudprovider.Offering,
	requested corev1.ResourceList,
) *v1alpha1.ExitClaim {
	tmpl := pool.Spec.Template.Spec
	reqs := append([]v1alpha1.NodeSelectorRequirementWithMinValues(nil), tmpl.Requirements...)
	reqs = append(reqs, scheduling.PinChosen(it, off)...)
	spec := v1alpha1.ExitClaimSpec{
		ProviderClassRef:       tmpl.ProviderClassRef,
		Requirements:           reqs,
		Frps:                   tmpl.Frps,
		Resources:              v1alpha1.ResourceRequirements{Requests: requested.DeepCopy()},
		ExpireAfter:            tmpl.ExpireAfter,
		TerminationGracePeriod: tmpl.TerminationGracePeriod,
	}
	return &v1alpha1.ExitClaim{Spec: spec}
}

// canPackOntoNewClaim simulates ResolveAutoAssign against the
// replacement claim's frps port set for the moveable tunnels. The
// replacement's Allocatable comes from the chosen instance type via
// scheduling.SelectInstanceType, which already filtered ResourcesFit;
// here we only need to verify ports.
func canPackOntoNewClaim(claim *v1alpha1.ExitClaim, tunnels []*v1alpha1.Tunnel) bool {
	used := map[int32]struct{}{}
	for _, t := range tunnels {
		if t == nil {
			continue
		}
		assigned, ok := scheduling.ResolveAutoAssign(
			claim.Spec.Frps.AllowPorts, claim.Spec.Frps.ReservedPorts,
			used, t.Spec.Ports,
		)
		if !ok {
			return false
		}
		for _, p := range assigned {
			used[p] = struct{}{}
		}
	}
	return true
}
