# Phase 2: CloudProvider Interface + Implementations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Define the `pkg/cloudprovider.CloudProvider` interface (mirroring Karpenter's), build an in-memory `fake` implementation used by every later test, and port the existing localdocker + digitalocean providers from `internal/provider/` to the new contract.

**Architecture:** One core interface package, one registry that dispatches by `ProviderClassRef.Kind`, and one sub-package per provider. Fake provider is the canonical reference implementation — every test in Phase 3-9 plugs it in.

**Tech Stack:** Go, github.com/docker/docker (Docker SDK), github.com/digitalocean/godo (DO SDK), github.com/onsi/ginkgo + onsi/gomega, sigs.k8s.io/controller-runtime fake clients.

**Spec section reference:** §9 (CloudProvider interface), §16 (test scaffolding).

**Prerequisite:** Phase 1 merged.

**End state:**
- `pkg/cloudprovider/types.go` defines `CloudProvider`, `InstanceType`, `Offering`, `DriftReason`, error types.
- `pkg/cloudprovider/fake/` implements the interface in-memory; all tests use it as the default.
- `pkg/cloudprovider/registry.go` dispatches to a per-`ProviderClassRef.Kind` impl.
- `pkg/cloudprovider/localdocker/` re-implements the localdocker provider against the new interface, consuming `LocalDockerProviderClass`.
- `pkg/cloudprovider/digitalocean/` re-implements the DO provider against the new interface, consuming `DigitalOceanProviderClass`.
- `pkg/cloudprovider/frps/` houses frps daemon config rendering and admin-client helpers (was `internal/frp/`).
- `make test` passes for `./pkg/cloudprovider/...`.

---

## File map

### Created

```
pkg/cloudprovider/
├── types.go                              # CloudProvider interface, InstanceType, Offering, DriftReason, errors, NodeLifecycleHook
├── registry.go                           # Multi-provider Registry; resolves ProviderClassRef → impl
├── errors.go                             # NotFoundError, InsufficientCapacityError, NewExitNotFoundError
├── doc.go
├── fake/
│   ├── cloudprovider.go                  # In-memory CloudProvider impl
│   ├── instancetype.go                   # Fake instance-type catalog
│   ├── providerclass.go                  # Fake ProviderClass type for tests
│   └── cloudprovider_test.go             # Round-trip tests for fake
├── frps/
│   ├── config.go                         # Renders frps.toml from FrpsConfig
│   ├── config_test.go
│   ├── admin/
│   │   ├── client.go                     # HTTP client for frps admin API
│   │   ├── proxies.go                    # GetProxies, AddProxy, DeleteProxy
│   │   ├── server_info.go                # GetServerInfo (used as registration probe)
│   │   └── admin_test.go                 # httptest-based unit tests
│   └── release/
│       ├── release.go                    # frps version → image-tag/binary-URL helpers
│       └── release_test.go
├── localdocker/
│   ├── cloudprovider.go                  # CloudProvider impl
│   ├── instancetype.go                   # Single fake instance-type ("local-1") with unbounded capacity
│   ├── docker.go                         # Docker SDK helpers (createContainer, removeContainer)
│   ├── docker_test.go                    # Uses dockertest if available, else fake; gated by build tag
│   └── (v1alpha1/ already created in Phase 1)
└── digitalocean/
    ├── cloudprovider.go
    ├── instancetype.go                   # Static catalog of DO sizes
    ├── droplet.go                        # godo.DropletCreate/Delete/Get wrappers
    ├── cloudinit.go                      # Renders cloud-init from FrpsConfig
    ├── cloudinit_test.go
    └── (v1alpha1/ already created in Phase 1)
```

### Modified

```
go.mod, go.sum                            # New dep: pkg/utils stuff that was in internal/configuration
                                          # (or: keep deps unchanged, just re-import current ones)
```

---

## Task 1: CloudProvider interface

**Files:**
- Create: `pkg/cloudprovider/types.go`
- Create: `pkg/cloudprovider/errors.go`
- Create: `pkg/cloudprovider/doc.go`

- [ ] **Step 1: Write `doc.go`**

```go
// Package cloudprovider defines the contract that every cloud-side
// provisioner must implement. Mirrors sigs.k8s.io/karpenter
// pkg/cloudprovider/types.go. Implementations live in sub-packages
// (fake/, localdocker/, digitalocean/, ...).
package cloudprovider
```

- [ ] **Step 2: Write `errors.go`**

```go
package cloudprovider

import (
	"errors"
	"fmt"
)

// ExitNotFoundError signals that the cloud-side resource is gone.
// Lifecycle.Delete returns this to stop retrying once the object is
// confirmed removed.
type ExitNotFoundError struct{ ProviderID string }

func (e *ExitNotFoundError) Error() string {
	return fmt.Sprintf("exit %q not found on provider", e.ProviderID)
}

// NewExitNotFoundError constructs the error.
func NewExitNotFoundError(providerID string) error {
	return &ExitNotFoundError{ProviderID: providerID}
}

// IsExitNotFound is the Errors.As-style helper consumers use.
func IsExitNotFound(err error) bool {
	var t *ExitNotFoundError
	return errors.As(err, &t)
}

// InsufficientCapacityError signals the provider couldn't fulfill the
// claim under current quota/availability. Provisioner treats this as
// retryable.
type InsufficientCapacityError struct{ Reason string }

func (e *InsufficientCapacityError) Error() string {
	return "insufficient capacity: " + e.Reason
}

func NewInsufficientCapacityError(reason string) error {
	return &InsufficientCapacityError{Reason: reason}
}

func IsInsufficientCapacity(err error) bool {
	var t *InsufficientCapacityError
	return errors.As(err, &t)
}
```

- [ ] **Step 3: Write `types.go`**

```go
package cloudprovider

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// CloudProvider is the per-provider contract. One implementation per
// ProviderClass kind. Resolved at runtime by Registry.
type CloudProvider interface {
	// Name identifies the provider (e.g. "local-docker", "digital-ocean").
	Name() string

	// Create launches a new exit. Implementations MUST return a hydrated
	// ExitClaim with Status.ProviderID, Status.ExitName, Status.Capacity,
	// Status.Allocatable, and Status.PublicIP populated when known.
	// On retry idempotency: if the provider already has an exit with
	// the same name, return the existing one (do not error).
	Create(ctx context.Context, claim *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error)

	// Delete tears down the exit. Returns ExitNotFoundError when the
	// resource is already gone — caller stops retrying on that signal.
	Delete(ctx context.Context, claim *v1alpha1.ExitClaim) error

	// Get returns the live state of an exit by ProviderID. Used by drift
	// detection and consistency reconciliation.
	Get(ctx context.Context, providerID string) (*v1alpha1.ExitClaim, error)

	// List enumerates all exits the provider knows about (used for GC).
	List(ctx context.Context) ([]*v1alpha1.ExitClaim, error)

	// GetInstanceTypes returns the instance-type catalog for a given
	// ExitPool. Pool's requirements may filter the catalog.
	GetInstanceTypes(ctx context.Context, pool *v1alpha1.ExitPool) ([]*InstanceType, error)

	// IsDrifted compares live cloud state against the claim's spec.
	// Returns a non-empty DriftReason if the exit no longer matches.
	IsDrifted(ctx context.Context, claim *v1alpha1.ExitClaim) (DriftReason, error)

	// RepairPolicies declares which Node-condition-driven repairs the
	// provider supports. Empty for v1.
	RepairPolicies() []RepairPolicy

	// GetSupportedProviderClasses returns the ProviderClass CRD types
	// this provider accepts. Used by the operator to register schemes
	// and watchers.
	GetSupportedProviderClasses() []client.Object
}

// InstanceType is one provisionable shape exposed by a provider.
type InstanceType struct {
	// Name identifies the shape (e.g. "s-1vcpu-1gb").
	Name string
	// Requirements pins shape-specific labels (region, capacity-type, ...).
	Requirements []v1alpha1.NodeSelectorRequirementWithMinValues
	// Offerings is the per-zone-and-capacity-type variant list with prices.
	Offerings Offerings
	// Capacity is the full ResourceList this instance type provides.
	Capacity corev1.ResourceList
	// Overhead is the system reservation subtracted from Capacity to
	// produce Allocatable.
	Overhead corev1.ResourceList
}

// Allocatable returns Capacity minus Overhead. Helper.
func (i *InstanceType) Allocatable() corev1.ResourceList {
	out := corev1.ResourceList{}
	for k, v := range i.Capacity {
		out[k] = v.DeepCopy()
	}
	for k, v := range i.Overhead {
		if cur, ok := out[k]; ok {
			cur.Sub(v)
			out[k] = cur
		}
	}
	return out
}

// Offerings is a list of variants available for an InstanceType.
type Offerings []*Offering

// Offering is one zone-and-capacity-type variant of an instance type.
type Offering struct {
	Requirements []v1alpha1.NodeSelectorRequirementWithMinValues
	Price        float64
	Available    bool
}

// DriftReason is a non-empty string when the cloud-side state diverges
// from the declarative claim.
type DriftReason string

// RepairPolicy declares which Node condition triggers an auto-repair.
type RepairPolicy struct {
	ConditionType   string
	ConditionStatus string
	TolerationDuration string
}

// NodeLifecycleHook is an optional extension. Providers may implement
// it to run custom logic at registration time (e.g. attach extra ENIs
// on AWS). Lifecycle controller invokes hooks after Registered=True.
type NodeLifecycleHook interface {
	Registered(ctx context.Context, claim *v1alpha1.ExitClaim) (NodeLifecycleHookResult, error)
}

type NodeLifecycleHookResult struct {
	// Requeue requests another reconcile after the named duration.
	Requeue *metav1Duration
}

// metav1Duration is a thin alias to avoid import cycles in tests.
type metav1Duration = v1alpha1.Duration
```

- [ ] **Step 4: Run go vet**

Run: `go vet ./pkg/cloudprovider/...`
Expected: PASS, no implementations yet but interface compiles.

- [ ] **Step 5: Commit**

```bash
git add pkg/cloudprovider/types.go pkg/cloudprovider/errors.go pkg/cloudprovider/doc.go
git commit -m "feat(cloudprovider): define CloudProvider interface + error types"
```

---

## Task 2: Registry (multi-provider dispatch)

**Files:**
- Create: `pkg/cloudprovider/registry.go`
- Create: `pkg/cloudprovider/registry_test.go`

- [ ] **Step 1: Write `registry_test.go`** (TDD — failing test first)

```go
package cloudprovider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
)

func TestRegistry_For_Found(t *testing.T) {
	reg := cloudprovider.NewRegistry()
	fp := fake.New()
	require.NoError(t, reg.Register("LocalDockerProviderClass", fp))

	got, err := reg.For("LocalDockerProviderClass")
	require.NoError(t, err)
	require.Same(t, fp, got)
}

func TestRegistry_For_Unknown(t *testing.T) {
	reg := cloudprovider.NewRegistry()
	_, err := reg.For("DoesNotExist")
	require.Error(t, err)
}

func TestRegistry_DoubleRegister_ReplacesAndWarns(t *testing.T) {
	reg := cloudprovider.NewRegistry()
	require.NoError(t, reg.Register("X", fake.New()))
	require.Error(t, reg.Register("X", fake.New())) // explicit error to avoid silent override
	_ = context.Background()
}
```

- [ ] **Step 2: Run; verify FAILS** (registry not yet defined)

Run: `go test ./pkg/cloudprovider/ -run TestRegistry -v`
Expected: compile error or NewRegistry undefined.

- [ ] **Step 3: Write `registry.go`**

```go
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

// Names lists all registered kinds. Stable order not guaranteed.
func (r *Registry) Kinds() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byKind))
	for k := range r.byKind {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 4: Run; verify FAILS at fake.New** (fake not built yet)

Run: `go test ./pkg/cloudprovider/ -run TestRegistry -v`
Expected: compile error: `fake.New undefined`. Move on to Task 3 (build fake first).

- [ ] **Step 5: Don't commit yet — Task 3 finishes the test.**

---

## Task 3: Fake CloudProvider (canonical reference impl)

**Files:**
- Create: `pkg/cloudprovider/fake/cloudprovider.go`
- Create: `pkg/cloudprovider/fake/instancetype.go`
- Create: `pkg/cloudprovider/fake/providerclass.go`
- Create: `pkg/cloudprovider/fake/cloudprovider_test.go`

- [ ] **Step 1: Write `fake/providerclass.go`**

```go
// Package fake provides an in-memory CloudProvider used by every
// controller test in pkg/controllers/. Mirrors Karpenter's
// pkg/cloudprovider/fake.
package fake

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FakeProviderClass is a stand-in CRD used in tests. The real localdocker
// and digitalocean providers ship their own CRDs.
//
// +kubebuilder:object:generate=true
type FakeProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}
```

- [ ] **Step 2: Write `fake/instancetype.go`**

```go
package fake

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// DefaultInstanceTypes returns the catalog every fake-backed test sees
// unless overridden.
func DefaultInstanceTypes() []*cloudprovider.InstanceType {
	return []*cloudprovider.InstanceType{
		{
			Name: "fake-small",
			Requirements: []v1alpha1.NodeSelectorRequirementWithMinValues{
				{Key: "frp.operator.io/region", Operator: v1alpha1.NodeSelectorOpIn, Values: []string{"fake-region-1"}},
			},
			Offerings: cloudprovider.Offerings{
				{Requirements: nil, Price: 0, Available: true},
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:                       resource.MustParse("1"),
				corev1.ResourceMemory:                    resource.MustParse("1Gi"),
				corev1.ResourceName("frp.operator.io/bandwidthMbps"):  resource.MustParse("1000"),
			},
			Overhead: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	}
}
```

- [ ] **Step 3: Write `fake/cloudprovider.go`** (full impl)

```go
package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// CloudProvider is the in-memory fake impl. Goroutine-safe.
type CloudProvider struct {
	mu        sync.RWMutex
	exits     map[string]*v1alpha1.ExitClaim // keyed by ProviderID
	driftMap  map[string]cloudprovider.DriftReason
	instances []*cloudprovider.InstanceType
	// Hooks for tests to inject failures.
	CreateFailure error
	DeleteFailure error
}

// New constructs a fake with default instance types.
func New() *CloudProvider {
	return &CloudProvider{
		exits:     map[string]*v1alpha1.ExitClaim{},
		driftMap:  map[string]cloudprovider.DriftReason{},
		instances: DefaultInstanceTypes(),
	}
}

func (c *CloudProvider) Name() string { return "fake" }

func (c *CloudProvider) Create(_ context.Context, claim *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error) {
	if c.CreateFailure != nil {
		return nil, c.CreateFailure
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Idempotency: same name → same ProviderID.
	for id, existing := range c.exits {
		if existing.Name == claim.Name {
			return c.cloneAndHydrate(claim, id), nil
		}
	}
	id := "fake://" + uuid.NewString()
	hydrated := c.cloneAndHydrate(claim, id)
	c.exits[id] = hydrated.DeepCopy()
	return hydrated, nil
}

func (c *CloudProvider) cloneAndHydrate(claim *v1alpha1.ExitClaim, providerID string) *v1alpha1.ExitClaim {
	out := claim.DeepCopy()
	out.Status.ProviderID = providerID
	out.Status.ExitName = "fake-exit-" + claim.Name
	out.Status.PublicIP = "203.0.113.1"
	out.Status.ImageID = "fake-image:" + claim.Spec.Frps.Version
	out.Status.FrpsVersion = claim.Spec.Frps.Version
	out.Status.Capacity = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1"),
		corev1.ResourceMemory: resource.MustParse("1Gi"),
		corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse("1000"),
	}
	out.Status.Allocatable = out.Status.Capacity.DeepCopy()
	return out
}

func (c *CloudProvider) Delete(_ context.Context, claim *v1alpha1.ExitClaim) error {
	if c.DeleteFailure != nil {
		return c.DeleteFailure
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.exits[claim.Status.ProviderID]; !ok {
		return cloudprovider.NewExitNotFoundError(claim.Status.ProviderID)
	}
	delete(c.exits, claim.Status.ProviderID)
	return nil
}

func (c *CloudProvider) Get(_ context.Context, providerID string) (*v1alpha1.ExitClaim, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	got, ok := c.exits[providerID]
	if !ok {
		return nil, cloudprovider.NewExitNotFoundError(providerID)
	}
	return got.DeepCopy(), nil
}

func (c *CloudProvider) List(_ context.Context) ([]*v1alpha1.ExitClaim, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*v1alpha1.ExitClaim, 0, len(c.exits))
	for _, e := range c.exits {
		out = append(out, e.DeepCopy())
	}
	return out, nil
}

func (c *CloudProvider) GetInstanceTypes(_ context.Context, _ *v1alpha1.ExitPool) ([]*cloudprovider.InstanceType, error) {
	return c.instances, nil
}

func (c *CloudProvider) IsDrifted(_ context.Context, claim *v1alpha1.ExitClaim) (cloudprovider.DriftReason, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.driftMap[claim.Status.ProviderID], nil
}

func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy { return nil }

func (c *CloudProvider) GetSupportedProviderClasses() []client.Object {
	return []client.Object{&FakeProviderClass{}}
}

// MarkDrifted is a test helper.
func (c *CloudProvider) MarkDrifted(providerID string, reason cloudprovider.DriftReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.driftMap[providerID] = reason
}

// Reset wipes all stored exits/drift. Useful between tests.
func (c *CloudProvider) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exits = map[string]*v1alpha1.ExitClaim{}
	c.driftMap = map[string]cloudprovider.DriftReason{}
}

// Errorf is a fmt-style helper to inject a typed CreateFailure during a test.
func (c *CloudProvider) ErrorOnCreate(format string, args ...interface{}) {
	c.CreateFailure = fmt.Errorf(format, args...)
}
```

- [ ] **Step 4: Write `fake/cloudprovider_test.go`**

```go
package fake_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
)

func newClaim(name string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1Object(name),
		Spec: v1alpha1.ExitClaimSpec{
			Frps: v1alpha1.FrpsConfig{Version: "v0.68.1", BindPort: 7000, AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
		},
	}
}

func TestFake_CreateGetDeleteRoundtrip(t *testing.T) {
	ctx := context.Background()
	cp := fake.New()
	in := newClaim("e1")

	out, err := cp.Create(ctx, in)
	require.NoError(t, err)
	require.NotEmpty(t, out.Status.ProviderID)
	require.Equal(t, "fake-exit-e1", out.Status.ExitName)

	got, err := cp.Get(ctx, out.Status.ProviderID)
	require.NoError(t, err)
	require.Equal(t, out.Status.ExitName, got.Status.ExitName)

	require.NoError(t, cp.Delete(ctx, out))
	_, err = cp.Get(ctx, out.Status.ProviderID)
	require.True(t, cloudprovider.IsExitNotFound(err))
}

func TestFake_Idempotent(t *testing.T) {
	ctx := context.Background()
	cp := fake.New()
	a, _ := cp.Create(ctx, newClaim("x"))
	b, _ := cp.Create(ctx, newClaim("x"))
	require.Equal(t, a.Status.ProviderID, b.Status.ProviderID)
}

func TestFake_InjectedFailure(t *testing.T) {
	ctx := context.Background()
	cp := fake.New()
	cp.ErrorOnCreate("simulated outage")
	_, err := cp.Create(ctx, newClaim("y"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "simulated outage")
}
```

NOTE: `metav1Object` is a small helper. Add at top of test file:

```go
import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

func metav1Object(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Name: name} }
```

- [ ] **Step 5: Run; verify PASS**

Run: `go test ./pkg/cloudprovider/fake/... -v`
Expected: 3 tests PASS.

- [ ] **Step 6: Re-run registry tests**

Run: `go test ./pkg/cloudprovider/ -run TestRegistry -v`
Expected: 3 tests PASS now that fake exists.

- [ ] **Step 7: Commit**

```bash
git add pkg/cloudprovider/registry.go pkg/cloudprovider/registry_test.go pkg/cloudprovider/fake/
git commit -m "feat(cloudprovider): registry + in-memory fake reference impl"
```

---

## Task 4: frps config rendering

**Files:**
- Create: `pkg/cloudprovider/frps/config.go`
- Create: `pkg/cloudprovider/frps/config_test.go`

This logic existed in `internal/frp/config/`. Re-implement against `v1alpha1.FrpsConfig`. Don't blindly copy — the new type has fewer fields per spec §3.

- [ ] **Step 1: Write `config_test.go`** (TDD)

```go
package frps_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps"
)

func TestRenderConfig_Minimal(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AdminPort:  7400,
		AllowPorts: []string{"80", "443", "1024-65535"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
	}
	out, err := frps.RenderConfig(cfg, "secret-token")
	require.NoError(t, err)
	require.Contains(t, out, "bindPort = 7000")
	require.Contains(t, out, "webServer.port = 7400")
	require.Contains(t, out, `auth.method = "token"`)
	require.Contains(t, out, `auth.token = "secret-token"`)
	require.True(t, strings.Contains(out, "allowPorts"))
}

func TestRenderConfig_TLS(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AllowPorts: []string{"443"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
		TLS:        &v1alpha1.FrpsTLSConfig{Force: true},
	}
	out, err := frps.RenderConfig(cfg, "tok")
	require.NoError(t, err)
	require.Contains(t, out, "transport.tls.force = true")
}
```

- [ ] **Step 2: Run; verify FAILS** (frps.RenderConfig undefined).

- [ ] **Step 3: Write `config.go`**

```go
// Package frps renders frps daemon configuration (TOML) from the
// declarative v1alpha1.FrpsConfig.
package frps

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// RenderConfig produces the frps.toml body for a FrpsConfig + auth token.
// The token is the resolved value (caller fetched from Secret).
func RenderConfig(cfg v1alpha1.FrpsConfig, authToken string) (string, error) {
	var b strings.Builder
	bindPort := cfg.BindPort
	if bindPort == 0 {
		bindPort = 7000
	}
	fmt.Fprintf(&b, "bindPort = %d\n", bindPort)
	if cfg.AdminPort != 0 {
		fmt.Fprintf(&b, "webServer.addr = \"0.0.0.0\"\n")
		fmt.Fprintf(&b, "webServer.port = %d\n", cfg.AdminPort)
	}
	if cfg.VhostHTTPPort != nil {
		fmt.Fprintf(&b, "vhostHTTPPort = %d\n", *cfg.VhostHTTPPort)
	}
	if cfg.VhostHTTPSPort != nil {
		fmt.Fprintf(&b, "vhostHTTPSPort = %d\n", *cfg.VhostHTTPSPort)
	}
	if cfg.KCPBindPort != nil {
		fmt.Fprintf(&b, "kcpBindPort = %d\n", *cfg.KCPBindPort)
	}
	if cfg.QUICBindPort != nil {
		fmt.Fprintf(&b, "quicBindPort = %d\n", *cfg.QUICBindPort)
	}
	if len(cfg.AllowPorts) > 0 {
		fmt.Fprintf(&b, "allowPorts = [")
		for i, p := range cfg.AllowPorts {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", p)
		}
		b.WriteString("]\n")
	}
	switch cfg.Auth.Method {
	case "token", "":
		b.WriteString("auth.method = \"token\"\n")
		fmt.Fprintf(&b, "auth.token = %q\n", authToken)
	default:
		return "", fmt.Errorf("unsupported auth method %q", cfg.Auth.Method)
	}
	if cfg.TLS != nil {
		if cfg.TLS.Force {
			b.WriteString("transport.tls.force = true\n")
		}
		// Real cert/key/ca pathing is wired by lifecycle controller via
		// volumes; here we only render flags it must honor.
	}
	return b.String(), nil
}
```

- [ ] **Step 4: Run; verify PASS**

Run: `go test ./pkg/cloudprovider/frps/ -v`
Expected: 2 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/cloudprovider/frps/config.go pkg/cloudprovider/frps/config_test.go
git commit -m "feat(frps): render frps.toml from v1alpha1.FrpsConfig"
```

---

## Task 5: frps admin client

**Files:**
- Create: `pkg/cloudprovider/frps/admin/client.go`
- Create: `pkg/cloudprovider/frps/admin/proxies.go`
- Create: `pkg/cloudprovider/frps/admin/server_info.go`
- Create: `pkg/cloudprovider/frps/admin/admin_test.go`

Same logic as `internal/frp/admin/` but consuming the new types. Use `httptest.Server` in tests.

- [ ] **Step 1: Write `client.go`**

```go
// Package admin is a thin HTTP client for the frps admin API.
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client targets one frps admin endpoint.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("admin %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
```

- [ ] **Step 2: Write `server_info.go`**

```go
package admin

import "context"

type ServerInfo struct {
	Version  string `json:"version"`
	BindPort int    `json:"bindPort"`
}

func (c *Client) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	var out ServerInfo
	if err := c.get(ctx, "/api/serverinfo", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

- [ ] **Step 3: Write `proxies.go`**

```go
package admin

import "context"

type Proxy struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	RemotePort int    `json:"remotePort,omitempty"`
}

func (c *Client) ListProxies(ctx context.Context) ([]Proxy, error) {
	var out struct {
		Proxies []Proxy `json:"proxies"`
	}
	if err := c.get(ctx, "/api/proxy/tcp", &out); err != nil {
		return nil, err
	}
	return out.Proxies, nil
}
```

- [ ] **Step 4: Write `admin_test.go`**

```go
package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
)

func TestGetServerInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/serverinfo", r.URL.Path)
		_, _ = w.Write([]byte(`{"version":"v0.68.1","bindPort":7000}`))
	}))
	defer srv.Close()

	c := admin.New(srv.URL)
	info, err := c.GetServerInfo(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v0.68.1", info.Version)
	require.Equal(t, 7000, info.BindPort)
}
```

- [ ] **Step 5: Run; verify PASS**

Run: `go test ./pkg/cloudprovider/frps/admin/ -v`
Expected: 1 test PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/cloudprovider/frps/admin/
git commit -m "feat(frps/admin): port admin client to new package"
```

---

## Task 6: Localdocker provider

**Files:**
- Create: `pkg/cloudprovider/localdocker/cloudprovider.go`
- Create: `pkg/cloudprovider/localdocker/instancetype.go`
- Create: `pkg/cloudprovider/localdocker/docker.go`
- Create: `pkg/cloudprovider/localdocker/cloudprovider_test.go`

Logic ported from `internal/provider/localdocker/`. Implements `cloudprovider.CloudProvider`. Reads `LocalDockerProviderClass` for config. Uses `frps.RenderConfig` + `frps/admin.Client`.

- [ ] **Step 1: Implement `instancetype.go`**

Single instance type "local-1" with effectively unbounded capacity. Localdocker has no cost / quota dimensions.

```go
package localdocker

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

func InstanceTypes() []*cloudprovider.InstanceType {
	return []*cloudprovider.InstanceType{
		{
			Name: "local-1",
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
				corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse("10000"),
			},
			Offerings: cloudprovider.Offerings{{Available: true, Price: 0}},
		},
	}
}
```

- [ ] **Step 2: Implement `docker.go`** (Docker SDK helpers)

Port from `internal/provider/localdocker/localdocker.go` — keep `containerName`, `createContainer`, `removeContainer`, `inspectContainer`, `dialAdminAPI` style helpers. Use the bind-mount pattern documented in spec (`Spec.ConfigHostMountPath`).

Skeleton:

```go
package localdocker

import (
	"context"

	"github.com/docker/docker/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

type dockerOps struct {
	cli *client.Client
}

func newDockerOps() (*dockerOps, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &dockerOps{cli: cli}, nil
}

// containerName produces the deterministic container name used both
// by Create (idempotency) and Get/Delete.
func containerName(claim *v1alpha1.ExitClaim) string {
	return "frp-operator-" + claim.Name
}

func (d *dockerOps) ensureContainer(ctx context.Context, claim *v1alpha1.ExitClaim, pc *ldv1alpha1.LocalDockerProviderClass) (string /* providerID */, error) {
	// 1. List existing containers; if name match, return its ID.
	// 2. Render frps.toml via frps.RenderConfig(claim.Spec.Frps, token).
	// 3. Write to filepath.Join(pc.Spec.ConfigHostMountPath, claim.Name+".toml").
	// 4. Compute port mappings, honoring SkipHostPortPublishing.
	// 5. ContainerCreate + ContainerStart.
	// 6. Return ProviderID = "localdocker://" + container ID.
	panic("port from internal/provider/localdocker/localdocker.go")
}
```

(Full body: 200-ish lines. Let the implementer subagent finish; the `panic` is a placeholder marker for them to replace before commit.)

NOTE TO IMPLEMENTER: replace the `panic` with the real implementation before committing this file. The TDD test below forces it.

- [ ] **Step 3: Implement `cloudprovider.go`** (the interface methods themselves)

```go
package localdocker

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

type CloudProvider struct {
	kube   client.Client
	docker *dockerOps
}

func New(kube client.Client) (*CloudProvider, error) {
	d, err := newDockerOps()
	if err != nil {
		return nil, err
	}
	return &CloudProvider{kube: kube, docker: d}, nil
}

func (c *CloudProvider) Name() string { return "local-docker" }

func (c *CloudProvider) Create(ctx context.Context, claim *v1alpha1.ExitClaim) (*v1alpha1.ExitClaim, error) {
	pc, err := c.resolveClass(ctx, claim)
	if err != nil {
		return nil, err
	}
	id, err := c.docker.ensureContainer(ctx, claim, pc)
	if err != nil {
		return nil, err
	}
	out := claim.DeepCopy()
	out.Status.ProviderID = id
	out.Status.ExitName = containerName(claim)
	out.Status.ImageID = "fatedier/frps:" + claim.Spec.Frps.Version
	out.Status.FrpsVersion = claim.Spec.Frps.Version
	// PublicIP populated by inspecting container network in real impl.
	return out, nil
}

func (c *CloudProvider) resolveClass(ctx context.Context, claim *v1alpha1.ExitClaim) (*ldv1alpha1.LocalDockerProviderClass, error) {
	if claim.Spec.ProviderClassRef.Kind != "LocalDockerProviderClass" {
		return nil, fmt.Errorf("localdocker: refusing kind %q", claim.Spec.ProviderClassRef.Kind)
	}
	var pc ldv1alpha1.LocalDockerProviderClass
	if err := c.kube.Get(ctx, client.ObjectKey{Name: claim.Spec.ProviderClassRef.Name}, &pc); err != nil {
		return nil, fmt.Errorf("get LocalDockerProviderClass %q: %w", claim.Spec.ProviderClassRef.Name, err)
	}
	return &pc, nil
}

func (c *CloudProvider) Delete(ctx context.Context, claim *v1alpha1.ExitClaim) error {
	return c.docker.removeContainer(ctx, claim)
}

func (c *CloudProvider) Get(ctx context.Context, providerID string) (*v1alpha1.ExitClaim, error) {
	return c.docker.inspectContainer(ctx, providerID)
}

func (c *CloudProvider) List(ctx context.Context) ([]*v1alpha1.ExitClaim, error) {
	return c.docker.listContainers(ctx)
}

func (c *CloudProvider) GetInstanceTypes(_ context.Context, _ *v1alpha1.ExitPool) ([]*cloudprovider.InstanceType, error) {
	return InstanceTypes(), nil
}

func (c *CloudProvider) IsDrifted(ctx context.Context, claim *v1alpha1.ExitClaim) (cloudprovider.DriftReason, error) {
	got, err := c.Get(ctx, claim.Status.ProviderID)
	if err != nil {
		if cloudprovider.IsExitNotFound(err) {
			return cloudprovider.DriftReason("Vanished"), nil
		}
		return "", err
	}
	if got.Spec.Frps.Version != claim.Spec.Frps.Version {
		return cloudprovider.DriftReason("FrpsVersionMismatch"), nil
	}
	return "", nil
}

func (c *CloudProvider) RepairPolicies() []cloudprovider.RepairPolicy { return nil }

func (c *CloudProvider) GetSupportedProviderClasses() []sigsk8sclient.Object {
	return []sigsk8sclient.Object{&ldv1alpha1.LocalDockerProviderClass{}}
}
```

(Replace `sigsk8sclient` with the real `sigs.k8s.io/controller-runtime/pkg/client` import alias.)

- [ ] **Step 4: Tests**

Use `dockertest`-style integration if a Docker socket is available; otherwise unit-test the helpers (containerName, port-mapping logic) directly. Do NOT spin up real Docker in this phase's CI; that's e2e (Phase 10).

```go
package localdocker_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker"
)

func TestContainerName_Deterministic(t *testing.T) {
	a := localdocker.ContainerNameForTest(&v1alpha1.ExitClaim{ /* set name */ })
	b := localdocker.ContainerNameForTest(&v1alpha1.ExitClaim{ /* same name */ })
	require.Equal(t, a, b)
}
```

(Expose `ContainerNameForTest` as `func ContainerNameForTest(claim *v1alpha1.ExitClaim) string { return containerName(claim) }` in a `_test_helpers.go` file with `//go:build !ignore_helpers`.)

- [ ] **Step 5: Run; verify PASS**

Run: `go test ./pkg/cloudprovider/localdocker/ -v`
Expected: PASS (helper-only tests; real Docker exercised in Phase 10).

- [ ] **Step 6: Commit**

```bash
git add pkg/cloudprovider/localdocker/
git commit -m "feat(localdocker): port provider against new CloudProvider interface"
```

---

## Task 7: DigitalOcean provider

**Files:**
- Create: `pkg/cloudprovider/digitalocean/cloudprovider.go`
- Create: `pkg/cloudprovider/digitalocean/instancetype.go`
- Create: `pkg/cloudprovider/digitalocean/droplet.go`
- Create: `pkg/cloudprovider/digitalocean/cloudinit.go`
- Create: `pkg/cloudprovider/digitalocean/cloudinit_test.go`
- Create: `pkg/cloudprovider/digitalocean/cloudprovider_test.go`

Same shape as Task 6 but against godo SDK + DigitalOceanProviderClass.

- [ ] **Step 1: Write `instancetype.go`**

Static catalog of DO sizes. Each maps a `Size` slug to cpu/memory/bandwidth Capacity. Subset for v1: `s-1vcpu-1gb`, `s-2vcpu-2gb`, `s-2vcpu-4gb`. Cite https://slugs.do-api.dev/.

- [ ] **Step 2: Write `cloudinit.go`**

Cloud-init script that installs frps from the GitHub release URL, drops a systemd unit, writes frps.toml from `frps.RenderConfig`, and starts the service.

- [ ] **Step 3: Write `cloudinit_test.go`**

Asserts the rendered cloud-init contains the expected version-pinned download URL, systemd unit, and ExecStart line referencing the rendered config path.

- [ ] **Step 4: Write `droplet.go`**

godo wrappers: `dropletCreate`, `dropletDelete`, `dropletGet`. Tagging convention: every operator-provisioned droplet gets `frp-operator-managed` tag for List/IsDrifted scoping.

- [ ] **Step 5: Write `cloudprovider.go`**

Same structure as localdocker. Idempotency on Create: tag-based lookup before creating.

- [ ] **Step 6: Tests run with godo's interface mocked. Use `pkg/cloudprovider/digitalocean/internal/godomock` if needed.**

- [ ] **Step 7: Commit**

```bash
git add pkg/cloudprovider/digitalocean/
git commit -m "feat(digitalocean): port provider against new CloudProvider interface"
```

---

## Task 8: Wire registry, sanity tests, finalize

**Files:**
- Modify: `pkg/cloudprovider/registry.go` (already done, no changes)
- Add: `pkg/cloudprovider/registry_integration_test.go` (smoke test that exercises all three providers via the Registry)

- [ ] **Step 1: Write smoke integration test**

```go
package cloudprovider_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker"
)

func TestRegistry_AllProvidersRegister(t *testing.T) {
	reg := cloudprovider.NewRegistry()

	// Fake always works.
	require.NoError(t, reg.Register("FakeProviderClass", fake.New()))

	// Real providers may fail if their backing systems aren't reachable.
	// Construction-only smoke test: just verify they expose the expected
	// supported-classes list. Don't call Create.
	if _, err := localdocker.New(nil); err == nil {
		require.NoError(t, reg.Register("LocalDockerProviderClass", &localdocker.CloudProvider{}))
	}
	if cp, err := digitalocean.New(nil, ""); err == nil {
		require.NoError(t, reg.Register("DigitalOceanProviderClass", cp))
	}

	got, err := reg.For("FakeProviderClass")
	require.NoError(t, err)
	require.NotNil(t, got)
}
```

- [ ] **Step 2: Run full pkg/cloudprovider tests**

Run: `go test ./pkg/cloudprovider/...`
Expected: ALL PASS.

- [ ] **Step 3: Run vet + build**

Run: `go vet ./pkg/cloudprovider/... && go build ./pkg/cloudprovider/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/cloudprovider/
git commit -m "test(cloudprovider): integration smoke test for registry+providers"
```

---

## Phase 2 acceptance checklist

- [x] `pkg/cloudprovider/types.go` defines `CloudProvider`, `InstanceType`, `Offering`, `DriftReason`, `RepairPolicy`, `NodeLifecycleHook`.
- [x] `pkg/cloudprovider/errors.go` defines `ExitNotFoundError`, `InsufficientCapacityError` + `IsExitNotFound`/`IsInsufficientCapacity` helpers.
- [x] `pkg/cloudprovider/registry.go` provides multi-provider dispatch.
- [x] `pkg/cloudprovider/fake/` is a fully-functional in-memory impl with Reset, MarkDrifted, ErrorOnCreate hooks.
- [x] `pkg/cloudprovider/frps/config.go` renders frps.toml from `v1alpha1.FrpsConfig`.
- [x] `pkg/cloudprovider/frps/admin/` provides the admin HTTP client.
- [x] `pkg/cloudprovider/localdocker/cloudprovider.go` implements the interface + helper tests pass.
- [x] `pkg/cloudprovider/digitalocean/cloudprovider.go` implements the interface + cloud-init renders correctly.
- [x] All `pkg/cloudprovider/...` tests pass under `go test`.
- [x] `go vet` clean; `go build` clean.
- [x] Multiple focused commits on the `karpenter-refactor` branch.

---

## Out-of-scope reminders

- No real Docker / DO API calls in this phase's tests. e2e in Phase 10.
- No controller wiring. CloudProvider impls are libraries; manager calls them in Phase 5.
- No idempotency-via-API-server work (the Phase 1 PR #5 label-breadcrumb pattern is gone in this rewrite — Phase 4 scheduler emits deterministic ExitClaim names; this phase is Just the cloud-provider boundary).
