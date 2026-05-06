package state

import (
	"sync"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// StatePool aggregates one ExitPool + the running totals of resources
// consumed by exits the pool has produced. The counter controller
// updates these fields via UpdatePool; readers must use Snapshot or
// SnapshotResources, both of which DeepCopy under the read lock.
//
// Resources and exits are unexported because the scheduler hot-path
// previously read them without locking — concurrent with the writer
// in cluster.UpdatePool — which raced on the underlying map. All
// access now goes through the snapshot helpers.
type StatePool struct {
	mu        sync.RWMutex
	Pool      *v1alpha1.ExitPool
	resources corev1.ResourceList
	exits     int64
}

// Snapshot returns a DeepCopy of the underlying pool object under the
// read lock. Callers receive a value safe to read concurrently with
// future writers.
func (p *StatePool) Snapshot() *v1alpha1.ExitPool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.Pool == nil {
		return nil
	}
	return p.Pool.DeepCopy()
}

// SnapshotResources returns a DeepCopy of the running resource total
// and the exit count, taken atomically under the read lock.
func (p *StatePool) SnapshotResources() (corev1.ResourceList, int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.resources.DeepCopy(), p.exits
}

// setResources is the internal writer used by cluster.UpdatePool. It
// stores a DeepCopy so a later mutation of the caller's map does not
// race with subsequent readers.
func (p *StatePool) setResources(resources corev1.ResourceList, exits int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resources = resources.DeepCopy()
	p.exits = exits
}
