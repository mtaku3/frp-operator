# Phase 3: Provisioner Interface and Fake/LocalDocker Implementations

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.

**Goal:** Land the `Provisioner` Go interface that future controllers will call to create / destroy / inspect cloud VPSes, plus two implementations:

- **Fake** — in-memory state machine. No external dependencies. Used by controller unit tests in Phase 5+ to drive the full reconcile loop without a cloud or Docker.
- **LocalDocker** — provisions `frps` containers on the operator's Docker host. Used by e2e tests and for local dev. Acceptably gated behind a build tag or runtime probe so unit tests don't require Docker.

The DigitalOcean implementation comes in Phase 9, after controllers exist.

**Architecture:**

```
internal/provider/provider.go         — Provisioner interface, ProviderState, errors
internal/provider/registry.go         — name → Provisioner registry; populated by main.go in Phase 9
internal/provider/fake/fake.go        — in-memory Fake; supports failure injection for tests
internal/provider/fake/fake_test.go   — unit tests for the Fake itself (state machine)
internal/provider/localdocker/        — real Docker provisioner
```

**Tech Stack:** Go 1.24+. For LocalDocker: `github.com/docker/docker/client` (Docker SDK for Go). The cloud-init bootstrap from Phase 2 is NOT used by LocalDocker — instead the LocalDocker driver mounts a frps.toml directly into the container via a tmpfs volume. Phase 2's `internal/frp/config` and `internal/frp/admin` remain the source of truth for config rendering.

**Reference spec:** [`docs/superpowers/specs/2026-04-23-frp-operator-design.md`](../specs/2026-04-23-frp-operator-design.md) §5 (extension points) and §10 (testing).

**Out of scope:** Real DigitalOcean provider (Phase 9), allocator/scheduling logic (Phase 4), controller integration (Phase 5+).

---

## File Structure

```
internal/provider/provider.go              # Provisioner interface, ProviderState, ErrNotFound
internal/provider/provider_test.go         # Type-only sanity tests (interface satisfaction)
internal/provider/registry.go              # Registry of named provisioners
internal/provider/registry_test.go         # Register/Lookup/duplicate-name behavior

internal/provider/fake/fake.go             # FakeProvisioner with failure injection
internal/provider/fake/fake_test.go        # State-machine tests

internal/provider/localdocker/localdocker.go     # Real Docker driver
internal/provider/localdocker/localdocker_test.go# Integration tests (skipped if no Docker)
```

**Boundaries.** `provider` defines the abstraction. `fake` is a leaf — depends on nothing else in this phase. `localdocker` depends on `provider` + the Docker SDK + `internal/frp/config` (to render frps.toml for the container) + `internal/frp/release` (for the FRP version, but for LocalDocker we mount the rendered config and run a published `fatedier/frp` Docker image rather than installing into a VM). Tests in `localdocker` skip when the Docker daemon isn't reachable.

---

## Task 1: Provisioner interface and registry

**Files:**
- Create: `internal/provider/provider.go`, `internal/provider/registry.go`
- Test: `internal/provider/provider_test.go`, `internal/provider/registry_test.go`

- [ ] **Step 1: Write failing tests**

`internal/provider/registry_test.go`:

```go
package provider

import (
	"context"
	"errors"
	"testing"
)

type stubProvisioner struct{ name string }

func (s *stubProvisioner) Name() string                                 { return s.name }
func (s *stubProvisioner) Create(_ context.Context, _ Spec) (State, error) { return State{}, nil }
func (s *stubProvisioner) Destroy(_ context.Context, _ string) error    { return nil }
func (s *stubProvisioner) Inspect(_ context.Context, _ string) (State, error) { return State{}, ErrNotFound }

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
```

`internal/provider/provider_test.go`:

```go
package provider

import "testing"

// TestProvisionerInterfaceShape is a compile-time sanity check that documents
// the contract: any type that satisfies the Provisioner interface MUST have
// these four methods. If this stops compiling, the interface changed and
// callers across the codebase need to be updated.
func TestProvisionerInterfaceShape(t *testing.T) {
	var _ Provisioner = (*stubProvisioner)(nil)
}
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/provider/ -v`
Expected: build fails — types undefined.

- [ ] **Step 3: Write `internal/provider/provider.go`**

```go
// Package provider defines the Provisioner abstraction the operator uses to
// create and destroy backing infrastructure (cloud VPSes, Docker containers,
// or pre-existing external instances).
//
// Concrete implementations live in subpackages: provider/fake, provider/localdocker,
// provider/digitalocean (added in Phase 9). They register themselves with the
// Registry at process start so SchedulingPolicy can select one by name.
package provider

import (
	"context"
	"errors"
)

// Spec is the desired configuration of one exit's underlying VPS / container.
// Fields not relevant to a given Provisioner (e.g., Region for LocalDocker)
// are ignored; the Provisioner's documentation states what it requires.
type Spec struct {
	// Name is the operator-assigned identifier; provisioners use it as a
	// stable handle to look up state across reconcile loops. Typically
	// "<namespace>__<exitserver-name>".
	Name string

	// Region is provider-specific (e.g., "nyc1"). LocalDocker ignores it.
	Region string

	// Size is provider-specific SKU (e.g., "s-1vcpu-1gb"). LocalDocker ignores it.
	Size string

	// CloudInitUserData is the rendered cloud-init user-data script (from
	// internal/bootstrap.Render). DigitalOcean passes it as droplet
	// user-data. LocalDocker doesn't use it (the container starts frps
	// directly from a mounted config).
	CloudInitUserData []byte

	// FrpsConfigTOML is the rendered frps.toml content. Used by LocalDocker
	// (mounted as a volume). Cloud providers receive this via cloud-init.
	FrpsConfigTOML []byte

	// Credentials is provider-specific (e.g., DO API token). Opaque blob;
	// the Registry lookup decides how to interpret it.
	Credentials []byte

	// AdminPort, BindPort: ports the operator must be able to reach.
	// LocalDocker publishes them on the host; cloud providers open them
	// via cloud-init firewall rules.
	AdminPort int
	BindPort  int
}

// State is the observed condition of one provisioned resource. Returned by
// Create (after creation completes) and Inspect (on demand).
type State struct {
	// ProviderID is a stable identifier the Provisioner can use to look up
	// the resource in subsequent calls (DO droplet ID, Docker container ID).
	ProviderID string

	// PublicIP is the address the operator and external clients dial.
	// LocalDocker returns "127.0.0.1".
	PublicIP string

	// Phase reports the lifecycle phase the underlying resource is in.
	Phase Phase

	// Reason is a human-readable note for debugging when Phase is Failed
	// or Unreachable. Empty when state is healthy.
	Reason string
}

// Phase enumerates the lifecycle states a Provisioner can report. These map
// 1:1 to ExitServer.status.phase, but the Provisioner deals only with the
// underlying infrastructure — health probes and finalizer handling live in
// the controller.
type Phase string

const (
	PhaseProvisioning Phase = "Provisioning"
	PhaseRunning      Phase = "Running"
	PhaseFailed       Phase = "Failed"
	PhaseGone         Phase = "Gone" // Inspected and confirmed destroyed
)

// Provisioner is implemented by each backend (DigitalOcean, LocalDocker,
// External, Fake). All methods are context-cancellable.
type Provisioner interface {
	// Name returns the provisioner's stable identifier. Must match the
	// SchedulingPolicy.spec.provider value used to select it.
	Name() string

	// Create makes the underlying resource. It blocks until the resource
	// is at least in PhaseProvisioning (the controller polls Inspect
	// afterwards). Returns the initial State (typically with ProviderID
	// populated, PublicIP empty until ready).
	Create(ctx context.Context, spec Spec) (State, error)

	// Destroy tears down the resource identified by providerID. Idempotent
	// — destroying an already-gone resource returns nil error. Bubble
	// non-recoverable errors so the caller can surface them on the CR.
	Destroy(ctx context.Context, providerID string) error

	// Inspect returns the current State of providerID. If the resource is
	// genuinely gone (deleted out of band), return ErrNotFound — the
	// controller treats that as PhaseLost. Transient errors (network
	// blips) should be returned as-is so the controller can retry.
	Inspect(ctx context.Context, providerID string) (State, error)
}

// Sentinel errors. Wrap with fmt.Errorf("...: %w", err) when adding context.
var (
	// ErrNotFound is returned by Inspect when the resource is confirmed
	// gone (e.g., DO 404, Docker container removed).
	ErrNotFound = errors.New("provider: resource not found")

	// ErrNotRegistered is returned by Registry.Lookup when no Provisioner
	// is registered under the requested name.
	ErrNotRegistered = errors.New("provider: not registered")
)
```

- [ ] **Step 4: Write `internal/provider/registry.go`**

```go
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
```

- [ ] **Step 5: Run, confirm PASS**

`devbox run -- go test ./internal/provider/ -v`
Expected: 4 tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/provider.go internal/provider/registry.go internal/provider/provider_test.go internal/provider/registry_test.go
git commit -m "feat(provider): Provisioner interface + Registry"
```

---

## Task 2: Fake provisioner

**Files:**
- Create: `internal/provider/fake/fake.go`
- Test: `internal/provider/fake/fake_test.go`

The Fake is an in-memory state machine that future controller tests can drive. It supports two failure-injection knobs (no real I/O, so it doesn't need to mock anything else):

- `FailCreateOnce` — make the next `Create` return an error.
- `FailInspectFor` — make `Inspect` return a configured error for a specific ProviderID.

Plus a `Clock` injection point for time-dependent state transitions (currently unused but reserved for Phase 5 health-probe tests).

- [ ] **Step 1: Write the test**

`internal/provider/fake/fake_test.go`:

```go
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
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/provider/fake/ -v`
Expected: build fails — package doesn't exist.

- [ ] **Step 3: Write `internal/provider/fake/fake.go`**

```go
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
```

- [ ] **Step 4: Run, confirm PASS**

`devbox run -- go test ./internal/provider/fake/ -v`
Expected: 5 tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/fake/
git commit -m "feat(provider/fake): in-memory Provisioner with failure injection"
```

---

## Task 3: LocalDocker provisioner

**Files:**
- Create: `internal/provider/localdocker/localdocker.go`
- Test: `internal/provider/localdocker/localdocker_test.go`
- Modify: `go.mod`, `go.sum`

The LocalDocker provisioner runs `frps` inside Docker containers on the operator's host. Tests skip when Docker isn't reachable — they're not part of the default CI unit-test set but run locally and in the e2e harness.

Strategy:

- Use `github.com/docker/docker/client` (the official Go SDK).
- Image: `fatedier/frp:<release.Version>` if available; otherwise extract `frps` from the official tarball at build time. **For Phase 3 we use the published `snowdreamtech/frps` image** (community fork that publishes images to Docker Hub; the official `fatedier/frp` repo doesn't ship Docker images). This is documented as a Phase-3 simplification; if/when the upstream publishes images, swap.
- Container ports: publish `BindPort` and `AdminPort` on `127.0.0.1`. Tests then dial `127.0.0.1:<adminPort>` to verify.
- Configuration: write `Spec.FrpsConfigTOML` to a temp file, bind-mount to `/etc/frp/frps.toml`.
- Container labeling: tag containers with `frp-operator.io/provider=local-docker` and `frp-operator.io/exit=<spec.Name>` for cleanup.

- [ ] **Step 1: Add Docker SDK dependency**

```bash
devbox run -- go get github.com/docker/docker/client@latest
devbox run -- go get github.com/docker/docker/api/types/container@latest
devbox run -- go mod tidy
```

- [ ] **Step 2: Write the test**

`internal/provider/localdocker/localdocker_test.go`:

```go
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
	defer d.Close()
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
	defer d.Close()
	if d.Name() != "ldtest" {
		t.Errorf("Name: got %q", d.Name())
	}
}

func TestLocalDocker_SatisfiesProvisioner(t *testing.T) {
	d, err := New(Config{Name: "compile-check"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer d.Close()
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
	defer d.Close()

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
		conn.Close()
	}

	// Destroy then verify Inspect returns ErrNotFound.
	if err := d.Destroy(ctx, st.ProviderID); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := d.Inspect(ctx, st.ProviderID); !errors.Is(err, provider.ErrNotFound) && !strings.Contains(strings.ToLower(err.Error()), "no such container") {
		t.Errorf("Inspect after Destroy: got %v, want ErrNotFound or no-such-container", err)
	}
}
```

- [ ] **Step 3: Run, confirm FAIL**

`devbox run -- go test ./internal/provider/localdocker/ -v`
Expected: build fails.

- [ ] **Step 4: Write `internal/provider/localdocker/localdocker.go`**

```go
// Package localdocker provisions frps instances as Docker containers on the
// operator's host. Intended for local development and e2e tests, NOT
// production.
//
// Containers are labeled with frp-operator.io/provider=local-docker and
// frp-operator.io/exit=<spec.Name> for cleanup. Ports are published on
// 127.0.0.1 so the operator and external clients can dial them.
//
// The frps.toml content from spec.FrpsConfigTOML is written to a temp file
// and bind-mounted into /etc/frp/frps.toml. The image must contain frps
// configured to read that path; the default image is snowdreamtech/frps,
// which does so.
package localdocker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"

	"github.com/mtaku3/frp-operator/internal/provider"
)

// Config controls the LocalDocker provisioner's runtime behavior. All
// fields are optional; zero values use the documented defaults.
type Config struct {
	// Name is the value Provisioner.Name() returns. Defaults to "local-docker".
	Name string

	// Image is the Docker image reference to run. Defaults to
	// "snowdreamtech/frps:0.68.1".
	Image string

	// HostBindIP controls the address the published ports bind to.
	// Defaults to "127.0.0.1" so the daemon doesn't expose them publicly.
	HostBindIP string
}

// LocalDocker is a Provisioner that runs frps as Docker containers.
type LocalDocker struct {
	cfg    Config
	client *client.Client
}

// New constructs a LocalDocker. Returns an error if Docker SDK initialization
// fails (e.g., bad DOCKER_HOST). Note that the daemon may still be
// unreachable; callers should Ping or attempt a Create to verify.
func New(cfg Config) (*LocalDocker, error) {
	if cfg.Name == "" {
		cfg.Name = "local-docker"
	}
	if cfg.Image == "" {
		cfg.Image = "snowdreamtech/frps:0.68.1"
	}
	if cfg.HostBindIP == "" {
		cfg.HostBindIP = "127.0.0.1"
	}
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &LocalDocker{cfg: cfg, client: c}, nil
}

// Close releases the Docker SDK client. Idempotent.
func (d *LocalDocker) Close() error {
	if d.client == nil {
		return nil
	}
	err := d.client.Close()
	d.client = nil
	return err
}

// Name implements provider.Provisioner.
func (d *LocalDocker) Name() string { return d.cfg.Name }

// Create writes spec.FrpsConfigTOML to a temp file, pulls the image (if not
// present), and starts a container with the bind-mount and published ports.
// Blocks until the container is running or an error is returned.
func (d *LocalDocker) Create(ctx context.Context, spec provider.Spec) (provider.State, error) {
	if len(spec.FrpsConfigTOML) == 0 {
		return provider.State{}, errors.New("localdocker: FrpsConfigTOML is required")
	}
	if spec.BindPort == 0 || spec.AdminPort == 0 {
		return provider.State{}, errors.New("localdocker: BindPort and AdminPort are required")
	}

	cfgPath, err := writeTempConfig(spec.FrpsConfigTOML)
	if err != nil {
		return provider.State{}, err
	}

	// Pull image if not present (silently swallow "already exists").
	rd, err := d.client.ImagePull(ctx, d.cfg.Image, image.PullOptions{})
	if err != nil {
		return provider.State{}, fmt.Errorf("image pull: %w", err)
	}
	_, _ = io.Copy(io.Discard, rd)
	_ = rd.Close()

	// Build port bindings: BindPort/tcp and AdminPort/tcp on 127.0.0.1.
	portSet := nat.PortSet{
		nat.Port(strconv.Itoa(spec.BindPort) + "/tcp"):  struct{}{},
		nat.Port(strconv.Itoa(spec.AdminPort) + "/tcp"): struct{}{},
	}
	portMap := nat.PortMap{
		nat.Port(strconv.Itoa(spec.BindPort) + "/tcp"): []nat.PortBinding{
			{HostIP: d.cfg.HostBindIP, HostPort: strconv.Itoa(spec.BindPort)},
		},
		nat.Port(strconv.Itoa(spec.AdminPort) + "/tcp"): []nat.PortBinding{
			{HostIP: d.cfg.HostBindIP, HostPort: strconv.Itoa(spec.AdminPort)},
		},
	}

	containerCfg := &container.Config{
		Image:        d.cfg.Image,
		ExposedPorts: portSet,
		Labels: map[string]string{
			"frp-operator.io/provider": d.cfg.Name,
			"frp-operator.io/exit":     spec.Name,
		},
	}
	hostCfg := &container.HostConfig{
		PortBindings: portMap,
		Mounts: []mount.Mount{{
			Type:     mount.TypeBind,
			Source:   cfgPath,
			Target:   "/etc/frp/frps.toml",
			ReadOnly: true,
		}},
		AutoRemove: false,
	}
	created, err := d.client.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, sanitize(spec.Name))
	if err != nil {
		return provider.State{}, fmt.Errorf("container create: %w", err)
	}
	if err := d.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return provider.State{}, fmt.Errorf("container start: %w", err)
	}

	return d.Inspect(ctx, created.ID)
}

// Inspect implements provider.Provisioner.
func (d *LocalDocker) Inspect(ctx context.Context, providerID string) (provider.State, error) {
	resp, err := d.client.ContainerInspect(ctx, providerID)
	if err != nil {
		// Distinguish "no such container" from other errors.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no such container") || strings.Contains(msg, "404") {
			return provider.State{Phase: provider.PhaseGone}, fmt.Errorf("%s: %w", providerID, provider.ErrNotFound)
		}
		return provider.State{}, fmt.Errorf("inspect: %w", err)
	}
	state := provider.State{
		ProviderID: resp.ID,
		PublicIP:   d.cfg.HostBindIP,
	}
	switch {
	case resp.State.Running:
		state.Phase = provider.PhaseRunning
	case resp.State.Status == "created":
		state.Phase = provider.PhaseProvisioning
	default:
		state.Phase = provider.PhaseFailed
		state.Reason = resp.State.Error
	}
	return state, nil
}

// Destroy stops and removes the container. Idempotent: missing containers
// are not an error.
func (d *LocalDocker) Destroy(ctx context.Context, providerID string) error {
	timeout := 5
	if err := d.client.ContainerStop(ctx, providerID, container.StopOptions{Timeout: &timeout}); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "no such container") && !strings.Contains(msg, "404") {
			return fmt.Errorf("stop: %w", err)
		}
	}
	if err := d.client.ContainerRemove(ctx, providerID, container.RemoveOptions{Force: true}); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "no such container") && !strings.Contains(msg, "404") {
			return fmt.Errorf("remove: %w", err)
		}
	}
	return nil
}

// writeTempConfig writes the rendered frps.toml to a per-container temp file
// the daemon can bind-mount. The caller is responsible for retaining the
// file for the container's lifetime; teardown via Destroy doesn't remove it
// (rely on tmpfs / OS cleanup).
func writeTempConfig(body []byte) (string, error) {
	dir, err := os.MkdirTemp("", "frp-operator-localdocker-")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	path := filepath.Join(dir, "frps.toml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

// sanitize converts an arbitrary name into a Docker-safe container name.
// Docker permits [a-zA-Z0-9_.-], starting with [a-zA-Z0-9].
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '.', c == '-':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 || (out[0] != '_' && !isAlnum(out[0])) {
		out = append([]byte{'x'}, out...)
	}
	return "frp-operator-" + string(out)
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// Compile-time check that *LocalDocker implements provider.Provisioner.
var _ provider.Provisioner = (*LocalDocker)(nil)

// Filter returns a Docker filter that matches all containers managed by
// this provisioner. Useful for cleanup scripts.
func (d *LocalDocker) Filter() filters.Args {
	f := filters.NewArgs()
	f.Add("label", "frp-operator.io/provider="+d.cfg.Name)
	return f
}
```

- [ ] **Step 5: Run, confirm tests pass**

`devbox run -- go test ./internal/provider/localdocker/ -v`

If Docker is reachable: 3 tests pass (Name, Compile-check, integration).
If Docker is NOT reachable: 2 tests pass + 1 SKIP (integration).

Either is acceptable. The integration test is the only one that needs a real daemon.

- [ ] **Step 6: Run the entire internal tree to confirm no regressions**

`devbox run -- go test ./internal/...`
Expected: every package passes; localdocker integration test may SKIP if no Docker.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/localdocker/ go.mod go.sum
git commit -m "feat(provider/localdocker): Docker-backed Provisioner for dev and e2e

Runs snowdreamtech/frps containers on the operator's Docker host. The
integration test skips when the Docker daemon is unreachable, so this
addition does not require Docker on every CI/dev environment."
```

---

## Phase 3 done — exit criteria

- `devbox run -- make test` passes (Phase 1 controllers + Phase 2 primitives + Phase 3 provider package).
- `internal/provider/` exposes `Provisioner`, `Spec`, `State`, `Phase`, `Registry`, `ErrNotFound`, `ErrNotRegistered`.
- `internal/provider/fake/` ships an in-memory implementation suitable for controller unit tests.
- `internal/provider/localdocker/` ships a Docker-backed implementation for e2e and dev. Integration test gracefully skips when Docker isn't reachable.
- No controller, CRD, or scheduling code touched.

The next plan (Phase 4: Allocator + ProvisionStrategy) is pure-logic and can be developed in parallel from this point on; Phase 5 (ExitServerController) consumes both.
