// Package provider — Registry implementation. See provider.go for the
// Provisioner interface this Registry indexes.
package provider

import (
	"fmt"
	"sync"
)

// Registry is a name → Provisioner index. It is safe for concurrent use:
// Register acquires a write lock, Lookup a read lock. The intended pattern
// is one Registry per operator process, populated at startup.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Provisioner
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Provisioner)}
}

// Register adds p under p.Name(). Returns an error if a provisioner is
// already registered under that name — duplicate registration is treated
// as a programming bug, not silently overwritten.
func (r *Registry) Register(p Provisioner) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	if _, ok := r.m[name]; ok {
		return fmt.Errorf("provider: %q already registered", name)
	}
	r.m[name] = p
	return nil
}

// Lookup returns the Provisioner registered under name, or ErrNotRegistered.
func (r *Registry) Lookup(name string) (Provisioner, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, ErrNotRegistered)
	}
	return p, nil
}

// Names returns the registered provisioner names in non-deterministic order.
// Useful for diagnostics / startup logging.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.m))
	for n := range r.m {
		out = append(out, n)
	}
	return out
}
