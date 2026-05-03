package localdocker

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mtaku3/frp-operator/internal/provider"
)

// dockerAvailable returns true if the Docker daemon is reachable.
// Tests skip otherwise — these are integration tests, not unit tests.
func dockerAvailable(t *testing.T) bool {
	t.Helper()
	if os.Getenv("FRP_OPERATOR_SKIP_DOCKER") != "" {
		return false
	}
	d, err := New(Config{}) // default options; reads DOCKER_HOST
	if err != nil {
		t.Logf("docker not available: %v", err)
		return false
	}
	defer func() { _ = d.Close() }()
	_, err = d.client.Ping(context.Background())
	if err != nil {
		t.Logf("docker ping failed: %v", err)
		return false
	}
	return true
}

func TestLocalDocker_NameMatchesConfig(t *testing.T) {
	d, err := New(Config{Name: "ldtest"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	if d.Name() != "ldtest" {
		t.Errorf("Name: got %q", d.Name())
	}
}

func TestLocalDocker_SatisfiesProvisioner(t *testing.T) {
	d, err := New(Config{Name: "compile-check"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()
	var _ provider.Provisioner = d
}

func TestLocalDocker_CreateInspectDestroy(t *testing.T) {
	if !dockerAvailable(t) {
		t.Skip("docker daemon not available; skipping integration test")
	}
	d, err := New(Config{Name: "ldtest", Image: "snowdreamtech/frps:0.68.1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = d.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	spec := provider.Spec{
		Name:           "ldtest__exit-1",
		FrpsConfigTOML: []byte("bindPort = 7000\nwebServer.addr = \"0.0.0.0\"\nwebServer.port = 7500\n"),
		BindPort:       7000,
		AdminPort:      7500,
	}

	st, err := d.Create(ctx, spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if err := d.Destroy(context.Background(), st.ProviderID); err != nil {
			t.Logf("cleanup Destroy: %v", err)
		}
	})

	if st.ProviderID == "" {
		t.Fatal("ProviderID empty")
	}

	// Wait for container to be Running.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got, err := d.Inspect(ctx, st.ProviderID)
		if err == nil && got.Phase == provider.PhaseRunning {
			st = got
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if st.Phase != provider.PhaseRunning {
		t.Fatalf("never reached Running: %+v", st)
	}

	// admin port should be reachable on 127.0.0.1.
	conn, err := net.DialTimeout("tcp", st.PublicIP+":7500", 3*time.Second)
	if err != nil {
		t.Errorf("dial admin port: %v", err)
	} else {
		_ = conn.Close()
	}

	// Destroy then verify Inspect returns ErrNotFound.
	if err := d.Destroy(ctx, st.ProviderID); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := d.Inspect(ctx, st.ProviderID); !errors.Is(err, provider.ErrNotFound) && !strings.Contains(strings.ToLower(err.Error()), "no such container") {
		t.Errorf("Inspect after Destroy: got %v, want ErrNotFound or no-such-container", err)
	}
}
