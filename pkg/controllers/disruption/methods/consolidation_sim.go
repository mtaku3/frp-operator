package methods

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/provisioning/scheduling"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Simulator decides whether the tunnels currently bound to a set of
// candidate exits could be re-packed onto the remaining ready exits in the
// cluster. Used by SingleNodeConsolidation and MultiNodeConsolidation.
type Simulator struct {
	Cluster    *state.Cluster
	KubeClient client.Client
}

func NewSimulator(c *state.Cluster, kube client.Client) *Simulator {
	return &Simulator{Cluster: c, KubeClient: kube}
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
