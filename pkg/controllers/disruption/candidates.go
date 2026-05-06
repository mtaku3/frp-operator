package disruption

import (
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// PoolLookup resolves a pool name into the cached ExitPool snapshot.
type PoolLookup func(name string) *v1alpha1.ExitPool

// GetCandidates walks every StateExit and returns the subset eligible to be
// considered as disruption candidates. Filters out:
//   - claims that have not yet reached Ready=True (provisioner is still working)
//   - claims with the do-not-disrupt annotation
//   - claims already in the deletion path (DeletionTimestamp set or
//     MarkedForDeletion in-memory)
//   - claims whose pool can't be resolved (pool was deleted)
//
// Sorted by ascending DisruptionCost so the cheapest exit gets considered first.
func GetCandidates(c *state.Cluster, poolByName PoolLookup) []*Candidate {
	if c == nil {
		return nil
	}
	out := []*Candidate{}
	for _, se := range c.Exits() {
		claim, _ := se.SnapshotForRead()
		if claim == nil {
			continue
		}
		if !isReady(claim) {
			continue
		}
		if claim.DeletionTimestamp != nil {
			continue
		}
		if _, ok := claim.Annotations[v1alpha1.AnnotationDoNotDisrupt]; ok {
			continue
		}
		if se.IsMarkedForDeletion() {
			continue
		}
		poolName := claim.Labels[v1alpha1.LabelExitPool]
		pool := poolByName(poolName)
		if pool == nil {
			continue
		}
		out = append(out, &Candidate{
			Claim:             claim,
			State:             se,
			Pool:              pool,
			DisruptionCost:    computeCost(claim),
			LastBindingChange: lastBindingChange(claim),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].DisruptionCost < out[j].DisruptionCost
	})
	return out
}

// computeCost returns a heuristic disruption cost. v1: cheapest are claims
// that are recently created (less amortized cost to throw away). Phase 7+
// can layer on traffic / utilization weighting.
func computeCost(claim *v1alpha1.ExitClaim) float64 {
	created := claim.CreationTimestamp.Time
	if created.IsZero() {
		return 0
	}
	return time.Since(created).Seconds()
}

// lastBindingChange returns the time of the most recent transition of the
// claim's Empty condition. Falls back to CreationTimestamp on the brief
// window before pkg/controllers/exitclaim/emptiness has stamped the
// condition. Authoritative path: emptiness controller writes Empty=True
// with LastTransitionTime when the claim transitions to zero bound
// tunnels; ConsolidateAfter is measured from that stamp.
func lastBindingChange(claim *v1alpha1.ExitClaim) time.Time {
	if claim == nil {
		return time.Time{}
	}
	for _, c := range claim.Status.Conditions {
		if c.Type == v1alpha1.ConditionTypeEmpty {
			return c.LastTransitionTime.Time
		}
	}
	return claim.CreationTimestamp.Time
}

func isReady(claim *v1alpha1.ExitClaim) bool {
	for _, c := range claim.Status.Conditions {
		if c.Type == v1alpha1.ConditionTypeReady && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
