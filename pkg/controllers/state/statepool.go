package state

import (
	"sync"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// StatePool aggregates one ExitPool + the running totals of resources
// consumed by exits the pool has produced. Counter controller updates
// these in Phase 7; for now Phase 3 stores the pool object only.
type StatePool struct {
	mu        sync.RWMutex
	Pool      *v1alpha1.ExitPool
	Resources corev1.ResourceList
	Exits     int64
}

func (p *StatePool) Snapshot() *v1alpha1.ExitPool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.Pool == nil {
		return nil
	}
	return p.Pool.DeepCopy()
}
