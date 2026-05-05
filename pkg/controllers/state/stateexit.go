package state

import (
	"sync"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// StateExit is the in-memory aggregate for one ExitClaim. The Allocations
// map is derived from Tunnels that have AssignedExit==Claim.Name; it is
// NOT persisted on the ExitClaim CR (per Karpenter convention).
type StateExit struct {
	mu    sync.RWMutex
	Claim *v1alpha1.ExitClaim

	// Allocations maps port → tunnel-key ("<ns>/<name>"). Updated by
	// state.Cluster as Tunnel statuses change. Local copy (not shared).
	Allocations map[int32]TunnelKey

	// MarkedForDeletion blocks new bindings while disruption queue
	// drains the exit.
	MarkedForDeletion bool

	// Nominated indicates this exit was selected by a recent Solve.
	// Used by disruption to ignore freshly-scheduled exits.
	Nominated bool

	// DisruptionCost is filled by the disruption controller while
	// computing consolidation candidates. Zeroed otherwise.
	DisruptionCost float64
}

// TunnelKey is "<namespace>/<name>".
type TunnelKey string

// Available returns Allocatable minus the resource sum of currently
// bound tunnels. Conservative: per-tunnel resource requests come from
// the persisted Tunnel.Spec.Resources.Requests.
func (s *StateExit) Available(boundTunnelRequests []corev1.ResourceList) corev1.ResourceList {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Claim == nil {
		return nil
	}
	out := s.Claim.Status.Allocatable.DeepCopy()
	for _, req := range boundTunnelRequests {
		for k, v := range req {
			if cur, ok := out[k]; ok {
				cur.Sub(v)
				out[k] = cur
			}
		}
	}
	return out
}

// UsedPorts returns a snapshot copy of the allocated port set.
func (s *StateExit) UsedPorts() map[int32]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[int32]struct{}, len(s.Allocations))
	for p := range s.Allocations {
		out[p] = struct{}{}
	}
	return out
}

// PortHolder returns the tunnel-key bound to the given port, or empty.
func (s *StateExit) PortHolder(p int32) TunnelKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Allocations[p]
}

// IsEmpty reports whether no tunnels are bound to this exit.
func (s *StateExit) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Allocations) == 0
}

// SnapshotForRead returns a goroutine-safe deep copy of the underlying
// claim and a clone of the allocations map. Helpers for callers who
// need to read both atomically.
func (s *StateExit) SnapshotForRead() (*v1alpha1.ExitClaim, map[int32]TunnelKey) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Claim == nil {
		return nil, nil
	}
	allocCopy := make(map[int32]TunnelKey, len(s.Allocations))
	for k, v := range s.Allocations {
		allocCopy[k] = v
	}
	return s.Claim.DeepCopy(), allocCopy
}
