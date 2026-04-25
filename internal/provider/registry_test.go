package provider

import (
	"context"
	"errors"
	"testing"
)

type stubProvisioner struct{ name string }

func (s *stubProvisioner) Name() string                                    { return s.name }
func (s *stubProvisioner) Create(_ context.Context, _ Spec) (State, error) { return State{}, nil }
func (s *stubProvisioner) Destroy(_ context.Context, _ string) error       { return nil }
func (s *stubProvisioner) Inspect(_ context.Context, _ string) (State, error) {
	return State{}, ErrNotFound
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&stubProvisioner{name: "fake"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, err := r.Lookup("fake")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Name() != "fake" {
		t.Errorf("got name %q", got.Name())
	}
}

func TestRegistry_DuplicateName(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&stubProvisioner{name: "dup"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(&stubProvisioner{name: "dup"})
	if err == nil {
		t.Fatal("expected duplicate error, got nil")
	}
}

func TestRegistry_LookupMissing(t *testing.T) {
	r := NewRegistry()
	_, err := r.Lookup("missing")
	if !errors.Is(err, ErrNotRegistered) {
		t.Errorf("got %v, want ErrNotRegistered", err)
	}
}
