package disruption

import (
	"context"
	"fmt"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// Validate re-checks a Command immediately before it is enqueued. It catches
// races where the candidate has changed between the Method's decision and the
// queue's execution: the do-not-disrupt annotation appearing, the claim
// already being deleted, or a freshly-bound tunnel making an empty candidate
// no-longer-empty.
func Validate(_ context.Context, c *state.Cluster, cmd *Command) error {
	if cmd == nil || len(cmd.Candidates) == 0 {
		return fmt.Errorf("empty command")
	}
	for _, cand := range cmd.Candidates {
		if cand == nil || cand.Claim == nil {
			return fmt.Errorf("candidate missing claim")
		}
		// Re-look up the StateExit fresh from the cluster. Mutations made
		// between candidate gathering and command dispatch surface here.
		se := c.ExitForName(cand.Claim.Name)
		if se == nil {
			return fmt.Errorf("candidate %s no longer in cache", cand.Claim.Name)
		}
		liveClaim, _ := se.SnapshotForRead()
		if liveClaim == nil {
			return fmt.Errorf("candidate %s has no claim", cand.Claim.Name)
		}
		if liveClaim.DeletionTimestamp != nil {
			return fmt.Errorf("candidate %s already being deleted", liveClaim.Name)
		}
		if _, ok := liveClaim.Annotations[v1alpha1.AnnotationDoNotDisrupt]; ok {
			return fmt.Errorf("candidate %s gained do-not-disrupt annotation", liveClaim.Name)
		}
		if se.IsMarkedForDeletion() {
			return fmt.Errorf("candidate %s already MarkedForDeletion", liveClaim.Name)
		}
		// Reason-specific re-checks. Empty must still be empty; other
		// reasons rely on the Method's ShouldDisrupt staying true, which
		// is the responsibility of the controller loop calling Validate
		// shortly after ComputeCommands.
		if cmd.Reason == v1alpha1.DisruptionReasonEmpty && !se.IsEmpty() {
			return fmt.Errorf("candidate %s no longer empty", liveClaim.Name)
		}
	}
	return nil
}
