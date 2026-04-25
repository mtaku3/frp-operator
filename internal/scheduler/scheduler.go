// Package scheduler holds the operator's pure decision logic: which existing
// ExitServer should host a new Tunnel (Allocator), and what new ExitServer
// to provision when none fits (ProvisionStrategy). It has no I/O, no
// Kubernetes client, and no controller code — controllers compose its
// outputs with side-effecting operations.
package scheduler

import (
	"errors"
	"fmt"
	"sync"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// AllocateInput is the data an Allocator needs to decide. Receiving a struct
// rather than positional args lets future fields be added without breaking
// implementations.
type AllocateInput struct {
	Tunnel *frpv1alpha1.Tunnel
	Exits  []frpv1alpha1.ExitServer
}

// AllocationDecision is the result of an Allocator. Exit is the chosen
// ExitServer (nil when no exit fits). Reason carries a short
// human-readable explanation suitable for an event/condition.
type AllocationDecision struct {
	Exit   *frpv1alpha1.ExitServer
	Reason string
}

// Allocator picks an ExitServer for a Tunnel from the supplied list, or
// returns a decision with Exit=nil and a Reason if none fit.
type Allocator interface {
	Name() string
	Allocate(in AllocateInput) (AllocationDecision, error)
}

// ProvisionInput is the data a ProvisionStrategy needs to decide whether to
// create a new ExitServer.
type ProvisionInput struct {
	Tunnel  *frpv1alpha1.Tunnel
	Policy  *frpv1alpha1.SchedulingPolicy
	Current []frpv1alpha1.ExitServer // all ExitServers in scope (cluster or namespace)
}

// ProvisionDecision is the result of a ProvisionStrategy. When Provision is
// true, Spec carries the desired new ExitServer's spec. When false, Reason
// explains why (BudgetExceeded, FixedPoolFull, etc.).
type ProvisionDecision struct {
	Provision bool
	Reason    string
	Spec      frpv1alpha1.ExitServerSpec
}

// ProvisionStrategy decides whether/what to provision when allocation fails.
type ProvisionStrategy interface {
	Name() string
	Plan(in ProvisionInput) (ProvisionDecision, error)
}

// AllocatorRegistry indexes Allocator implementations by name. Mirror of
// internal/provider.Registry. Populated in main.go at startup.
type AllocatorRegistry struct {
	mu sync.RWMutex
	m  map[string]Allocator
}

// NewAllocatorRegistry returns an empty registry.
func NewAllocatorRegistry() *AllocatorRegistry {
	return &AllocatorRegistry{m: make(map[string]Allocator)}
}

// Register adds a by Name. Duplicate names error.
func (r *AllocatorRegistry) Register(a Allocator) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[a.Name()]; ok {
		return fmt.Errorf("scheduler: allocator %q already registered", a.Name())
	}
	r.m[a.Name()] = a
	return nil
}

// Lookup returns the Allocator registered under name, or ErrNotRegistered.
func (r *AllocatorRegistry) Lookup(name string) (Allocator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, ErrNotRegistered)
	}
	return a, nil
}

// ProvisionStrategyRegistry — symmetric to AllocatorRegistry.
type ProvisionStrategyRegistry struct {
	mu sync.RWMutex
	m  map[string]ProvisionStrategy
}

// NewProvisionStrategyRegistry returns an empty registry.
func NewProvisionStrategyRegistry() *ProvisionStrategyRegistry {
	return &ProvisionStrategyRegistry{m: make(map[string]ProvisionStrategy)}
}

// Register adds p by Name. Duplicate names error.
func (r *ProvisionStrategyRegistry) Register(p ProvisionStrategy) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[p.Name()]; ok {
		return fmt.Errorf("scheduler: provision strategy %q already registered", p.Name())
	}
	r.m[p.Name()] = p
	return nil
}

// Lookup returns the ProvisionStrategy registered under name.
func (r *ProvisionStrategyRegistry) Lookup(name string) (ProvisionStrategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("%q: %w", name, ErrNotRegistered)
	}
	return p, nil
}

// ErrNotRegistered is returned by Registry.Lookup when no implementation is
// registered under the requested name.
var ErrNotRegistered = errors.New("scheduler: not registered")
