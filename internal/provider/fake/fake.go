// Package fake provides an in-memory Provisioner implementation suitable
// for unit-testing controllers without a real cloud or Docker daemon.
//
// The Fake supports failure injection via FailCreateOnce so tests can
// exercise error paths deterministically. It is NOT safe for production —
// data is held in process memory and lost on restart.
package fake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/mtaku3/frp-operator/internal/provider"
)

// FakeProvisioner satisfies provider.Provisioner with an in-memory map of
// resources. Method receivers acquire a single mutex for simplicity; the
// Fake is intended for tests and is not optimized for concurrent throughput.
type FakeProvisioner struct {
	mu             sync.Mutex
	name           string
	resources      map[string]provider.State // providerID → state
	failCreateOnce error
}

// New returns a fresh FakeProvisioner whose Name() returns the given string.
func New(name string) *FakeProvisioner {
	return &FakeProvisioner{
		name:      name,
		resources: make(map[string]provider.State),
	}
}

// Name implements provider.Provisioner.
func (f *FakeProvisioner) Name() string { return f.name }

// FailCreateOnce sets a one-shot error: the next Create call returns it,
// then the failure is cleared. Useful for asserting controller error paths.
func (f *FakeProvisioner) FailCreateOnce(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failCreateOnce = err
}

// Create implements provider.Provisioner. Generates a random hex ID and
// records the resource in PhaseRunning at 127.0.0.1.
func (f *FakeProvisioner) Create(_ context.Context, spec provider.Spec) (provider.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreateOnce != nil {
		err := f.failCreateOnce
		f.failCreateOnce = nil
		return provider.State{}, err
	}
	id := newID()
	st := provider.State{
		ProviderID: id,
		PublicIP:   "127.0.0.1",
		Phase:      provider.PhaseRunning,
	}
	f.resources[id] = st
	return st, nil
}

// Inspect implements provider.Provisioner.
func (f *FakeProvisioner) Inspect(_ context.Context, providerID string) (provider.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.resources[providerID]
	if !ok {
		return provider.State{}, fmt.Errorf("inspect %q: %w", providerID, provider.ErrNotFound)
	}
	return st, nil
}

// Destroy implements provider.Provisioner. Idempotent: deleting an already
// absent resource returns nil.
func (f *FakeProvisioner) Destroy(_ context.Context, providerID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.resources, providerID)
	return nil
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is so unlikely on Linux that returning an
		// error from a constructor would just bloat the surface. Panic.
		panic(fmt.Sprintf("fake: rand: %v", err))
	}
	return "fake-" + hex.EncodeToString(b)
}
