package controller

import (
	"time"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// reclaimAction is the pure decision a ReclaimController makes for one
// reconcile pass. The reconciler translates each into client calls.
type reclaimAction int

const (
	// reclaimActionNoOp means: do nothing, requeue at the policy probe interval.
	reclaimActionNoOp reclaimAction = iota
	// reclaimActionStartDrain means: transition Ready -> Draining and stamp DrainStartedAt.
	reclaimActionStartDrain
	// reclaimActionAbortDrain means: transition Draining -> Ready and clear DrainStartedAt.
	reclaimActionAbortDrain
	// reclaimActionRequeueDrain means: keep Draining, requeue when remaining drain time elapses.
	reclaimActionRequeueDrain
	// reclaimActionDestroy means: drainAfter has elapsed; delete the CR (the
	// ExitServer's finalizer will run Provisioner.Destroy).
	reclaimActionDestroy
)

// reclaimAnnotation is the per-exit opt-out. Setting it to "false" disables
// reclaim for this exit regardless of policy.
const reclaimAnnotation = "frp-operator.io/reclaim"

// decideReclaim picks the next action for one ExitServer. Pure function.
//
// reclaimEnabled is the SchedulingPolicy's consolidation.reclaimEmpty value
// (default true if no policy or unset). drainAfter is the same policy's
// drainAfter duration. now is the current wall-clock time the controller
// uses to compare against DrainStartedAt.
func decideReclaim(exit *frpv1alpha1.ExitServer, reclaimEnabled bool, drainAfter time.Duration, now time.Time) reclaimAction {
	// Per-exit override beats policy.
	if val, ok := exit.Annotations[reclaimAnnotation]; ok && val == "false" {
		return reclaimActionNoOp
	}
	if !reclaimEnabled {
		return reclaimActionNoOp
	}
	switch exit.Status.Phase {
	case frpv1alpha1.PhaseReady:
		if len(exit.Status.Allocations) == 0 {
			return reclaimActionStartDrain
		}
		return reclaimActionNoOp
	case frpv1alpha1.PhaseDraining:
		if len(exit.Status.Allocations) > 0 {
			return reclaimActionAbortDrain
		}
		if exit.Status.DrainStartedAt == nil {
			// Inconsistent state — recover by re-stamping. Treat as
			// RequeueDrain so the next reconcile sees the timestamp.
			return reclaimActionStartDrain
		}
		elapsed := now.Sub(exit.Status.DrainStartedAt.Time)
		if elapsed >= drainAfter {
			return reclaimActionDestroy
		}
		return reclaimActionRequeueDrain
	default:
		// Pending/Provisioning/Unreachable/Lost/Deleting: the reclaim
		// controller is not in charge of these; ExitServerController
		// handles them.
		return reclaimActionNoOp
	}
}
