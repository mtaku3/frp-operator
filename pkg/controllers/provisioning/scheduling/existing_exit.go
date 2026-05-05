package scheduling

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// ExistingExit wraps state.StateExit with a per-Solve mutable record of
// "tunnels we'd bind onto it during this Solve" so subsequent CanAdd
// calls see consumed capacity.
type ExistingExit struct {
	State          *state.StateExit
	NewlyBound     []*v1alpha1.Tunnel
	NewlyUsedPorts map[int32]struct{}
}

// CanAdd checks whether the given tunnel may be placed on this exit and
// returns the resolved port assignment on success.
func (e *ExistingExit) CanAdd(tunnel *v1alpha1.Tunnel) ([]int32, error) {
	claim, used := e.snapshotClaimAndUsed()
	if claim == nil {
		return nil, fmt.Errorf("exit has no claim")
	}
	// Liveness gates.
	if e.State.MarkedForDeletion {
		return nil, fmt.Errorf("exit %s marked for deletion", claim.Name)
	}
	if claim.GetDeletionTimestamp() != nil {
		return nil, fmt.Errorf("exit %s being deleted", claim.Name)
	}
	if !readyTrue(claim.Status.Conditions) {
		return nil, fmt.Errorf("exit %s not Ready", claim.Name)
	}

	// Requirements compatibility.
	if err := Compatible(claim.Spec.Requirements, tunnel.Spec.Requirements); err != nil {
		return nil, err
	}

	// Resources fit.
	avail := claim.Status.Allocatable.DeepCopy()
	avail = Subtract(avail, sumTunnelRequests(e.NewlyBound))
	if !ResourcesFit(avail, tunnel.Spec.Resources.Requests) {
		return nil, fmt.Errorf("resources don't fit on %s", claim.Name)
	}

	// Ports fit.
	mergedUsed := mergePortSets(used, e.NewlyUsedPorts)
	assigned, ok := ResolveAutoAssign(claim.Spec.Frps.AllowPorts, claim.Spec.Frps.ReservedPorts, mergedUsed, tunnel.Spec.Ports)
	if !ok {
		return nil, fmt.Errorf("ports don't fit on %s", claim.Name)
	}
	return assigned, nil
}

// Add records the binding inside this Solve so subsequent CanAdd calls
// see the consumed capacity.
func (e *ExistingExit) Add(tunnel *v1alpha1.Tunnel, assignedPorts []int32) {
	e.NewlyBound = append(e.NewlyBound, tunnel)
	if e.NewlyUsedPorts == nil {
		e.NewlyUsedPorts = map[int32]struct{}{}
	}
	for _, p := range assignedPorts {
		e.NewlyUsedPorts[p] = struct{}{}
	}
}

func (e *ExistingExit) snapshotClaimAndUsed() (*v1alpha1.ExitClaim, map[int32]struct{}) {
	if e.State == nil {
		return nil, nil
	}
	used := e.State.UsedPorts()
	se := e.State
	if se.Claim == nil {
		return nil, used
	}
	return se.Claim.DeepCopy(), used
}

func mergePortSets(a, b map[int32]struct{}) map[int32]struct{} {
	if len(b) == 0 {
		return a
	}
	out := make(map[int32]struct{}, len(a)+len(b))
	for k := range a {
		out[k] = struct{}{}
	}
	for k := range b {
		out[k] = struct{}{}
	}
	return out
}

func sumTunnelRequests(tunnels []*v1alpha1.Tunnel) corev1.ResourceList {
	lists := make([]corev1.ResourceList, 0, len(tunnels))
	for _, t := range tunnels {
		if t == nil {
			continue
		}
		lists = append(lists, t.Spec.Resources.Requests)
	}
	return Sum(lists...)
}

func readyTrue(conds []metav1.Condition) bool {
	for _, c := range conds {
		if c.Type == "Ready" && c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
