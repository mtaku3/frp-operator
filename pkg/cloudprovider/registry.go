package cloudprovider

import (
	"fmt"
	"sync"
)

// Registry resolves a ProviderClassRef.Kind to a CloudProvider impl.
type Registry struct {
	mu     sync.RWMutex
	byKind map[string]CloudProvider
}

func NewRegistry() *Registry {
	return &Registry{byKind: map[string]CloudProvider{}}
}

// Register installs an impl under a ProviderClass kind.
// Returns error if kind already registered (no silent overrides).
func (r *Registry) Register(kind string, p CloudProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byKind[kind]; ok {
		return fmt.Errorf("cloudprovider: kind %q already registered", kind)
	}
	r.byKind[kind] = p
	return nil
}

// For returns the impl registered for a ProviderClass kind.
func (r *Registry) For(kind string) (CloudProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byKind[kind]
	if !ok {
		return nil, fmt.Errorf("cloudprovider: no impl registered for kind %q", kind)
	}
	return p, nil
}

// Kinds lists all registered kinds. Stable order not guaranteed.
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byKind))
	for k := range r.byKind {
		out = append(out, k)
	}
	return out
}
