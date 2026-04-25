package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/mtaku3/frp-operator/internal/provider"
)

func TestFake_CreateInspectDestroy(t *testing.T) {
	f := New("fake-test")
	ctx := context.Background()

	// Create → Running, returns an ID.
	st, err := f.Create(ctx, provider.Spec{Name: "ns__exit-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if st.ProviderID == "" {
		t.Fatal("ProviderID empty")
	}
	if st.Phase != provider.PhaseRunning {
		t.Errorf("Phase: got %v want Running", st.Phase)
	}
	if st.PublicIP != "127.0.0.1" {
		t.Errorf("PublicIP: got %q", st.PublicIP)
	}

	// Inspect: same state.
	st2, err := f.Inspect(ctx, st.ProviderID)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if st2.ProviderID != st.ProviderID || st2.Phase != provider.PhaseRunning {
		t.Errorf("Inspect mismatch: %+v", st2)
	}

	// Destroy: succeeds; subsequent Inspect returns ErrNotFound.
	if err := f.Destroy(ctx, st.ProviderID); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := f.Inspect(ctx, st.ProviderID); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Inspect after Destroy: got %v, want ErrNotFound", err)
	}

	// Destroy again: idempotent (no error).
	if err := f.Destroy(ctx, st.ProviderID); err != nil {
		t.Errorf("Destroy idempotent: got %v", err)
	}
}

func TestFake_FailCreateOnce(t *testing.T) {
	f := New("fake-fail")
	f.FailCreateOnce(errors.New("synthetic"))
	ctx := context.Background()
	if _, err := f.Create(ctx, provider.Spec{Name: "x"}); err == nil {
		t.Fatal("expected synthetic error")
	}
	// Subsequent Create succeeds.
	st, err := f.Create(ctx, provider.Spec{Name: "x"})
	if err != nil {
		t.Fatalf("Create after one-shot fail: %v", err)
	}
	if st.Phase != provider.PhaseRunning {
		t.Errorf("Phase: got %v", st.Phase)
	}
}

func TestFake_InspectMissingReturnsErrNotFound(t *testing.T) {
	f := New("fake-missing")
	if _, err := f.Inspect(context.Background(), "no-such-id"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestFake_NameMatchesConstructor(t *testing.T) {
	f := New("custom-name")
	if f.Name() != "custom-name" {
		t.Errorf("Name: got %q", f.Name())
	}
}

func TestFake_SatisfiesProvisioner(t *testing.T) {
	var _ provider.Provisioner = New("compile-check")
}
