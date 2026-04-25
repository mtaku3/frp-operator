# Phase 4: Allocator + ProvisionStrategy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.

**Goal:** Pure-Go scheduling logic that the upcoming `TunnelController` (Phase 6) calls to decide (a) which `ExitServer` should host a new `Tunnel`, and (b) what new `ExitServer` to provision when no existing one fits. Both behaviors are pluggable Go interfaces with built-in implementations.

**Architecture:** Single package `internal/scheduler/` with two interfaces and five built-in implementations. No I/O, no Kubernetes client, no controller code. Inputs are the v1alpha1 CRD Go types (already defined). Outputs are decisions the controller acts on.

**Tech Stack:** Go 1.24+, stdlib `testing` with table-driven tests. Imports `internal/api/v1alpha1` (CRD types) and `k8s.io/apimachinery` (for `metav1` if we touch ObjectMeta — typically we don't, just spec/status).

**Reference spec:** [`docs/superpowers/specs/2026-04-23-frp-operator-design.md`](../specs/2026-04-23-frp-operator-design.md) §4 (allocation and provisioning) and §5 (extension points). Phase-1 plan delivered the CRDs that this phase consumes.

**Out of scope:** Reservation persistence (Phase 6 — controller writes ports to ExitServer.status), live exit health (Phase 5), CRDs (Phase 1, done), real provider integration (Phase 9).

---

## File Structure

```
internal/scheduler/scheduler.go              — Allocator + ProvisionStrategy interfaces, decision types, registry
internal/scheduler/scheduler_test.go         — interface-shape tests, registry tests
internal/scheduler/eligibility.go            — shared filter helpers (placement match, port-fit, capacity, phase)
internal/scheduler/eligibility_test.go       — table-driven tests for filters
internal/scheduler/allocator_binpack.go      — BinPack
internal/scheduler/allocator_spread.go       — Spread
internal/scheduler/allocator_capacity.go     — CapacityAware (default)
internal/scheduler/allocator_test.go         — table-driven, all three
internal/scheduler/provision_ondemand.go     — OnDemand
internal/scheduler/provision_fixedpool.go    — FixedPool
internal/scheduler/provision_test.go         — table-driven, both
```

**Boundaries.** The scheduler package depends on `api/v1alpha1` for CR types. It does NOT depend on `internal/provider` (the controller composes scheduler decisions with provider calls). It does NOT depend on `internal/frp/*` (rendering happens in the controller layer). Pure decisions.

---

## Task 1: Scheduler interfaces + eligibility filters

**Files:**
- Create: `internal/scheduler/scheduler.go`, `internal/scheduler/eligibility.go`
- Test: `internal/scheduler/scheduler_test.go`, `internal/scheduler/eligibility_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/scheduler/scheduler_test.go`:

```go
package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

type stubAllocator struct{ name string }

func (s *stubAllocator) Name() string { return s.name }
func (s *stubAllocator) Allocate(_ AllocateInput) (AllocationDecision, error) {
	return AllocationDecision{}, nil
}

type stubProvisionStrategy struct{ name string }

func (s *stubProvisionStrategy) Name() string { return s.name }
func (s *stubProvisionStrategy) Plan(_ ProvisionInput) (ProvisionDecision, error) {
	return ProvisionDecision{}, nil
}

func TestInterfacesShape(t *testing.T) {
	var _ Allocator = (*stubAllocator)(nil)
	var _ ProvisionStrategy = (*stubProvisionStrategy)(nil)
}

func TestAllocatorRegistry(t *testing.T) {
	r := NewAllocatorRegistry()
	if err := r.Register(&stubAllocator{name: "x"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(&stubAllocator{name: "x"}); err == nil {
		t.Fatal("expected duplicate error")
	}
	got, err := r.Lookup("x")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Name() != "x" {
		t.Errorf("got %q", got.Name())
	}
	if _, err := r.Lookup("missing"); err == nil {
		t.Error("expected lookup miss to error")
	}
}

func TestAllocationDecisionZero(t *testing.T) {
	var d AllocationDecision
	if d.Exit != nil {
		t.Errorf("zero AllocationDecision.Exit must be nil")
	}
	if d.Reason != "" {
		t.Errorf("zero Reason must be empty")
	}
}

// TestProvisionDecisionWithSpec is a sanity check that the spec-carrying
// branch round-trips through reasonable use without losing information.
func TestProvisionDecisionWithSpec(t *testing.T) {
	d := ProvisionDecision{
		Provision: true,
		Spec: frpv1alpha1.ExitServerSpec{
			Provider: frpv1alpha1.ProviderDigitalOcean,
			Region:   "nyc1",
		},
	}
	if d.Spec.Provider != frpv1alpha1.ProviderDigitalOcean {
		t.Errorf("Spec.Provider lost")
	}
}
```

`internal/scheduler/eligibility_test.go`:

```go
package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeExit(name string, opts ...func(*frpv1alpha1.ExitServer)) frpv1alpha1.ExitServer {
	e := frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: frpv1alpha1.ExitServerSpec{
			Provider: frpv1alpha1.ProviderDigitalOcean,
			Region:   "nyc1",
		},
		Status: frpv1alpha1.ExitServerStatus{
			Phase:       frpv1alpha1.PhaseReady,
			Allocations: map[string]string{},
			Usage:       frpv1alpha1.ExitUsage{},
		},
	}
	for _, o := range opts {
		o(&e)
	}
	return e
}

func TestPortsFitWithReserved(t *testing.T) {
	exit := makeExit("e", func(e *frpv1alpha1.ExitServer) {
		e.Spec.ReservedPorts = []int32{22, 7000, 7500}
		e.Status.Allocations = map[string]string{"443": "ns/used"}
	})
	cases := []struct {
		name  string
		ports []int32
		fit   bool
	}{
		{"all free", []int32{80}, true},
		{"one already allocated", []int32{443, 80}, false},
		{"reserved port", []int32{22, 80}, false},
		{"empty ports trivially fit", []int32{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PortsFit(exit, tc.ports)
			if got != tc.fit {
				t.Errorf("got %v want %v", got, tc.fit)
			}
		})
	}
}

func TestCapacityFits(t *testing.T) {
	mt := int32(10)
	mtg := int64(100)
	bw := int32(500)
	exit := makeExit("e", func(e *frpv1alpha1.ExitServer) {
		e.Spec.Capacity = &frpv1alpha1.ExitCapacity{
			MaxTunnels: &mt, MonthlyTrafficGB: &mtg, BandwidthMbps: &bw,
		}
		e.Status.Usage = frpv1alpha1.ExitUsage{
			Tunnels: 9, MonthlyTrafficGB: 80, BandwidthMbps: 400,
		}
	})
	cases := []struct {
		name string
		req  frpv1alpha1.TunnelRequirements
		fit  bool
	}{
		{"empty req fits", frpv1alpha1.TunnelRequirements{}, true},
		{
			"one more tunnel fits exactly",
			frpv1alpha1.TunnelRequirements{
				MonthlyTrafficGB: ptrInt64(20), BandwidthMbps: ptrInt32(100),
			},
			true,
		},
		{
			"would exceed traffic",
			frpv1alpha1.TunnelRequirements{
				MonthlyTrafficGB: ptrInt64(21),
			},
			false,
		},
		{
			"would exceed bandwidth",
			frpv1alpha1.TunnelRequirements{
				BandwidthMbps: ptrInt32(101),
			},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CapacityFits(exit, tc.req)
			if got != tc.fit {
				t.Errorf("got %v want %v", got, tc.fit)
			}
		})
	}
}

func TestPlacementMatches(t *testing.T) {
	exit := makeExit("e")
	cases := []struct {
		name      string
		placement *frpv1alpha1.Placement
		match     bool
	}{
		{"nil placement matches anything", nil, true},
		{"matching provider", &frpv1alpha1.Placement{
			Providers: []frpv1alpha1.Provider{frpv1alpha1.ProviderDigitalOcean},
		}, true},
		{"non-matching provider", &frpv1alpha1.Placement{
			Providers: []frpv1alpha1.Provider{frpv1alpha1.ProviderLocalDocker},
		}, false},
		{"matching region", &frpv1alpha1.Placement{
			Regions: []string{"nyc1", "sfo3"},
		}, true},
		{"non-matching region", &frpv1alpha1.Placement{
			Regions: []string{"sfo3"},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlacementMatches(exit, tc.placement)
			if got != tc.match {
				t.Errorf("got %v want %v", got, tc.match)
			}
		})
	}
}

func TestEligibleExits(t *testing.T) {
	mt := int32(50)
	exits := []frpv1alpha1.ExitServer{
		makeExit("ready"),
		makeExit("provisioning", func(e *frpv1alpha1.ExitServer) { e.Status.Phase = frpv1alpha1.PhaseProvisioning }),
		makeExit("draining", func(e *frpv1alpha1.ExitServer) { e.Status.Phase = frpv1alpha1.PhaseDraining }),
		makeExit("at-cap", func(e *frpv1alpha1.ExitServer) {
			e.Spec.Capacity = &frpv1alpha1.ExitCapacity{MaxTunnels: &mt}
			e.Status.Usage.Tunnels = 50
		}),
	}
	tunnel := &frpv1alpha1.Tunnel{
		Spec: frpv1alpha1.TunnelSpec{
			Ports: []frpv1alpha1.TunnelPort{{Name: "h", ServicePort: 80}},
		},
	}
	got := EligibleExits(exits, tunnel)
	if len(got) != 1 || got[0].Name != "ready" {
		var names []string
		for _, e := range got {
			names = append(names, e.Name)
		}
		t.Errorf("expected only [ready]; got %v", names)
	}
}

// helpers — note these helpers are NEW for this package and do NOT collide
// with the api/v1alpha1 ptrInt32/ptrInt64 (different package).
func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/scheduler/ -v` (with `dangerouslyDisableSandbox: true`)
Expected: build fails — package missing.

- [ ] **Step 3: Write `internal/scheduler/scheduler.go`**

```go
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
```

- [ ] **Step 4: Write `internal/scheduler/eligibility.go`**

```go
package scheduler

import (
	"strconv"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// PortsFit returns true iff every requested public port is free on exit:
// not in spec.ReservedPorts and not already allocated in status.Allocations.
// An empty ports slice trivially fits.
func PortsFit(exit frpv1alpha1.ExitServer, ports []int32) bool {
	reserved := make(map[int32]struct{}, len(exit.Spec.ReservedPorts))
	for _, p := range exit.Spec.ReservedPorts {
		reserved[p] = struct{}{}
	}
	for _, p := range ports {
		if _, isReserved := reserved[p]; isReserved {
			return false
		}
		if _, allocated := exit.Status.Allocations[strconv.Itoa(int(p))]; allocated {
			return false
		}
	}
	return true
}

// CapacityFits returns true iff adding `req` to `exit.Status.Usage` stays
// at or below `exit.Spec.Capacity` for every dimension. Unset capacity
// dimensions are unbounded; unset requirement dimensions count as zero.
func CapacityFits(exit frpv1alpha1.ExitServer, req frpv1alpha1.TunnelRequirements) bool {
	cap := exit.Spec.Capacity
	use := exit.Status.Usage
	if cap == nil {
		return true
	}
	if cap.MaxTunnels != nil && use.Tunnels+1 > *cap.MaxTunnels {
		return false
	}
	if cap.MonthlyTrafficGB != nil {
		var add int64
		if req.MonthlyTrafficGB != nil {
			add = *req.MonthlyTrafficGB
		}
		if use.MonthlyTrafficGB+add > *cap.MonthlyTrafficGB {
			return false
		}
	}
	if cap.BandwidthMbps != nil {
		var add int32
		if req.BandwidthMbps != nil {
			add = *req.BandwidthMbps
		}
		if use.BandwidthMbps+add > *cap.BandwidthMbps {
			return false
		}
	}
	return true
}

// PlacementMatches returns true iff exit satisfies the soft preferences in
// placement. A nil placement is treated as no constraints. Lists are
// match-any: the exit qualifies if it appears in the list (or the list is
// empty for that dimension).
func PlacementMatches(exit frpv1alpha1.ExitServer, p *frpv1alpha1.Placement) bool {
	if p == nil {
		return true
	}
	if len(p.Providers) > 0 && !containsProvider(p.Providers, exit.Spec.Provider) {
		return false
	}
	if len(p.Regions) > 0 && !containsString(p.Regions, exit.Spec.Region) {
		return false
	}
	return true
}

func containsProvider(haystack []frpv1alpha1.Provider, needle frpv1alpha1.Provider) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// EligibleExits returns the subset of exits that are ready, port-compatible,
// capacity-compatible, and match the tunnel's placement preferences. The
// caller (Allocator) ranks among these.
func EligibleExits(exits []frpv1alpha1.ExitServer, t *frpv1alpha1.Tunnel) []frpv1alpha1.ExitServer {
	ports := tunnelPorts(t)
	var req frpv1alpha1.TunnelRequirements
	if t.Spec.Requirements != nil {
		req = *t.Spec.Requirements
	}
	out := make([]frpv1alpha1.ExitServer, 0, len(exits))
	for _, e := range exits {
		if e.Status.Phase != frpv1alpha1.PhaseReady {
			continue
		}
		if !PlacementMatches(e, t.Spec.Placement) {
			continue
		}
		if !PortsFit(e, ports) {
			continue
		}
		if !CapacityFits(e, req) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// tunnelPorts returns the public ports requested by the tunnel. PublicPort
// defaults to ServicePort when unset.
func tunnelPorts(t *frpv1alpha1.Tunnel) []int32 {
	out := make([]int32, 0, len(t.Spec.Ports))
	for _, p := range t.Spec.Ports {
		if p.PublicPort != nil {
			out = append(out, *p.PublicPort)
		} else {
			out = append(out, p.ServicePort)
		}
	}
	return out
}
```

- [ ] **Step 5: Run, confirm PASS**

`devbox run -- go test ./internal/scheduler/ -v`
Expected: all tests pass (TestInterfacesShape, TestAllocatorRegistry, TestAllocationDecisionZero, TestProvisionDecisionWithSpec, TestPortsFitWithReserved/<4>, TestCapacityFits/<4>, TestPlacementMatches/<5>, TestEligibleExits).

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/eligibility.go internal/scheduler/scheduler_test.go internal/scheduler/eligibility_test.go
git commit -m "feat(scheduler): Allocator + ProvisionStrategy interfaces and eligibility helpers"
```

---

## Task 2: Allocator implementations

**Files:**
- Create: `internal/scheduler/allocator_binpack.go`, `allocator_spread.go`, `allocator_capacity.go`
- Test: `internal/scheduler/allocator_test.go`

Three allocators, all sharing the eligibility filter from Task 1. They differ only in how they rank the eligible candidates.

- **BinPack** — prefers the *most* loaded eligible exit (densest first; minimizes exit count). Score: count of allocated ports + tunnel count.
- **Spread** — prefers the *least* loaded eligible exit (sparsest first; better fault isolation).
- **CapacityAware** — same eligibility as the helpers, then BinPack tie-breaking. The default.

- [ ] **Step 1: Write the test (table-driven)**

`internal/scheduler/allocator_test.go`:

```go
package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// allocatorTestExits builds a deterministic three-exit fleet for ranking
// tests. exit-empty has no allocations; exit-half has 5 allocations and 1
// tunnel; exit-full has 10 allocations and 2 tunnels.
func allocatorTestExits() []frpv1alpha1.ExitServer {
	mk := func(name string, allocs int, tunnels int32) frpv1alpha1.ExitServer {
		a := map[string]string{}
		for i := 0; i < allocs; i++ {
			a[itoa(20000+i)] = "ns/x"
		}
		return frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:   frpv1alpha1.ProviderDigitalOcean,
				Region:     "nyc1",
				AllowPorts: []string{"1024-65535"},
			},
			Status: frpv1alpha1.ExitServerStatus{
				Phase:       frpv1alpha1.PhaseReady,
				Allocations: a,
				Usage:       frpv1alpha1.ExitUsage{Tunnels: tunnels},
			},
		}
	}
	return []frpv1alpha1.ExitServer{
		mk("exit-empty", 0, 0),
		mk("exit-half", 5, 1),
		mk("exit-full", 10, 2),
	}
}

func itoa(n int) string { return string([]byte{byte('0' + (n/10000)%10), byte('0' + (n/1000)%10), byte('0' + (n/100)%10), byte('0' + (n/10)%10), byte('0' + n%10)}) }

func basicTunnel(port int32) *frpv1alpha1.Tunnel {
	return &frpv1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Name: "t", Namespace: "default"},
		Spec: frpv1alpha1.TunnelSpec{
			Service: frpv1alpha1.ServiceRef{Name: "s", Namespace: "default"},
			Ports:   []frpv1alpha1.TunnelPort{{Name: "p", ServicePort: port}},
		},
	}
}

func TestAllocators(t *testing.T) {
	exits := allocatorTestExits()
	tunnel := basicTunnel(80)

	cases := []struct {
		name      string
		allocator Allocator
		wantExit  string // empty → expect Exit==nil
	}{
		{"BinPack picks fullest eligible exit", &BinPackAllocator{}, "exit-full"},
		{"Spread picks emptiest eligible exit", &SpreadAllocator{}, "exit-empty"},
		{"CapacityAware default = BinPack ranking", &CapacityAwareAllocator{}, "exit-full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := tc.allocator.Allocate(AllocateInput{Tunnel: tunnel, Exits: exits})
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if d.Exit == nil {
				t.Fatalf("got nil exit; reason=%q", d.Reason)
			}
			if d.Exit.Name != tc.wantExit {
				t.Errorf("got %q want %q (reason=%q)", d.Exit.Name, tc.wantExit, d.Reason)
			}
		})
	}
}

func TestAllocators_NoEligibleExitReturnsReason(t *testing.T) {
	tunnel := basicTunnel(80)
	// All exits in PhaseProvisioning → none eligible.
	exits := allocatorTestExits()
	for i := range exits {
		exits[i].Status.Phase = frpv1alpha1.PhaseProvisioning
	}
	cases := []Allocator{&BinPackAllocator{}, &SpreadAllocator{}, &CapacityAwareAllocator{}}
	for _, a := range cases {
		t.Run(a.Name(), func(t *testing.T) {
			d, err := a.Allocate(AllocateInput{Tunnel: tunnel, Exits: exits})
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if d.Exit != nil {
				t.Errorf("expected Exit==nil, got %v", d.Exit.Name)
			}
			if d.Reason == "" {
				t.Error("expected non-empty Reason")
			}
		})
	}
}

func TestAllocators_PortConflictExcludesExit(t *testing.T) {
	// Block port 80 on exit-full. BinPack should fall back to exit-half.
	exits := allocatorTestExits()
	exits[2].Status.Allocations["80"] = "ns/blocker"

	tunnel := basicTunnel(80)
	d, err := (&BinPackAllocator{}).Allocate(AllocateInput{Tunnel: tunnel, Exits: exits})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if d.Exit == nil || d.Exit.Name != "exit-half" {
		var got string
		if d.Exit != nil {
			got = d.Exit.Name
		}
		t.Errorf("got %q want exit-half (reason=%q)", got, d.Reason)
	}
}

func TestAllocators_NamesAreStable(t *testing.T) {
	if (&BinPackAllocator{}).Name() != "BinPack" {
		t.Error()
	}
	if (&SpreadAllocator{}).Name() != "Spread" {
		t.Error()
	}
	if (&CapacityAwareAllocator{}).Name() != "CapacityAware" {
		t.Error()
	}
}
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/scheduler/ -run TestAllocators -v`
Expected: build fails — types undefined.

- [ ] **Step 3: Write `internal/scheduler/allocator_binpack.go`**

```go
package scheduler

import "sort"

// BinPackAllocator prefers the densest eligible exit, defined as the one
// with the most existing port allocations plus tunnel count. Ties broken
// by name (stable iteration). Use when minimizing exit count is the goal.
type BinPackAllocator struct{}

// Name implements Allocator.
func (BinPackAllocator) Name() string { return "BinPack" }

// Allocate implements Allocator.
func (BinPackAllocator) Allocate(in AllocateInput) (AllocationDecision, error) {
	eligible := EligibleExits(in.Exits, in.Tunnel)
	if len(eligible) == 0 {
		return AllocationDecision{Reason: "no eligible exit (port conflict, capacity, placement, or none Ready)"}, nil
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		di := exitDensity(eligible[i])
		dj := exitDensity(eligible[j])
		if di != dj {
			return di > dj // densest first
		}
		return eligible[i].Name < eligible[j].Name
	})
	return AllocationDecision{Exit: &eligible[0]}, nil
}

// exitDensity is the BinPack ranking score: existing allocations + tunnel
// count. Higher = more loaded.
func exitDensity(e exitServer) int {
	return len(e.Status.Allocations) + int(e.Status.Usage.Tunnels)
}

// exitServer is a type alias so the score function reads cleanly without
// dragging the api/v1alpha1 import label around.
//
//nolint:revive  // intentional internal alias
type exitServer = struct {
	exitServerInner
}

// We keep the alias trick honest by NOT redeclaring the struct shape — the
// Allocator implementations operate on api/v1alpha1.ExitServer values
// directly, so this file uses the actual type below.
```

**STOP.** The above pseudo-alias trick is a footgun. Rewrite `allocator_binpack.go` with the actual `frpv1alpha1.ExitServer` type — no fake aliases:

```go
package scheduler

import (
	"sort"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// BinPackAllocator prefers the densest eligible exit (most allocations +
// most tunnels). Ties broken by name for deterministic output.
type BinPackAllocator struct{}

// Name implements Allocator.
func (BinPackAllocator) Name() string { return "BinPack" }

// Allocate implements Allocator.
func (BinPackAllocator) Allocate(in AllocateInput) (AllocationDecision, error) {
	eligible := EligibleExits(in.Exits, in.Tunnel)
	if len(eligible) == 0 {
		return AllocationDecision{Reason: "no eligible exit (port conflict, capacity, placement, or none Ready)"}, nil
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		di := density(eligible[i])
		dj := density(eligible[j])
		if di != dj {
			return di > dj
		}
		return eligible[i].Name < eligible[j].Name
	})
	chosen := eligible[0]
	return AllocationDecision{Exit: &chosen}, nil
}

// density is the BinPack ranking score: a higher score means a more loaded
// exit. Tunnels and allocations are weighted equally — both signal load.
func density(e frpv1alpha1.ExitServer) int {
	return len(e.Status.Allocations) + int(e.Status.Usage.Tunnels)
}
```

- [ ] **Step 4: Write `internal/scheduler/allocator_spread.go`**

```go
package scheduler

import "sort"

// SpreadAllocator prefers the least-loaded eligible exit (fewest allocations
// + tunnels). Use when fault isolation matters more than minimizing exit
// count. Ties broken by name.
type SpreadAllocator struct{}

// Name implements Allocator.
func (SpreadAllocator) Name() string { return "Spread" }

// Allocate implements Allocator.
func (SpreadAllocator) Allocate(in AllocateInput) (AllocationDecision, error) {
	eligible := EligibleExits(in.Exits, in.Tunnel)
	if len(eligible) == 0 {
		return AllocationDecision{Reason: "no eligible exit (port conflict, capacity, placement, or none Ready)"}, nil
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		di := density(eligible[i])
		dj := density(eligible[j])
		if di != dj {
			return di < dj
		}
		return eligible[i].Name < eligible[j].Name
	})
	chosen := eligible[0]
	return AllocationDecision{Exit: &chosen}, nil
}
```

- [ ] **Step 5: Write `internal/scheduler/allocator_capacity.go`**

```go
package scheduler

// CapacityAwareAllocator is the default. Eligibility filtering already
// rejects over-capacity exits (see EligibleExits in eligibility.go); this
// allocator's only job is to pick among the eligible by BinPack ranking
// (densest first), giving us "fill-up-cheap-exits-first" without ever
// over-committing.
type CapacityAwareAllocator struct{}

// Name implements Allocator.
func (CapacityAwareAllocator) Name() string { return "CapacityAware" }

// Allocate implements Allocator. Delegates to BinPack ranking after the
// shared eligibility filter has done its work.
func (CapacityAwareAllocator) Allocate(in AllocateInput) (AllocationDecision, error) {
	return BinPackAllocator{}.Allocate(in)
}
```

- [ ] **Step 6: Run, confirm PASS**

`devbox run -- go test ./internal/scheduler/ -v`
Expected: all tests pass (Task 1 + Task 2 tests).

- [ ] **Step 7: Commit**

```bash
git add internal/scheduler/allocator_binpack.go internal/scheduler/allocator_spread.go internal/scheduler/allocator_capacity.go internal/scheduler/allocator_test.go
git commit -m "feat(scheduler): BinPack, Spread, CapacityAware allocators"
```

---

## Task 3: ProvisionStrategy implementations

**Files:**
- Create: `internal/scheduler/provision_ondemand.go`, `provision_fixedpool.go`
- Test: `internal/scheduler/provision_test.go`

Two strategies:

- **OnDemand** (default) — provisions one new exit per allocation failure. Uses `policy.spec.vps.default` for the spec, overridden by `tunnel.spec.placement.sizeOverride` and the placement region preferences. Refuses if `policy.spec.budget.maxExits` (or maxExitsPerNamespace) cap is hit.
- **FixedPool** — never provisions beyond a fixed count. Same budget logic as OnDemand. Decision is identical when `len(current) < maxExits`; otherwise refuses.

For v1, `OnDemand` and `FixedPool` end up sharing most logic — the only difference is whether they ever provision when within budget. We can implement them as one strategy with a knob, but to keep the registry mapping clean we split them into two types that compose a shared helper.

- [ ] **Step 1: Write the test**

`internal/scheduler/provision_test.go`:

```go
package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func basicPolicy(maxExits *int32) *frpv1alpha1.SchedulingPolicy {
	return &frpv1alpha1.SchedulingPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: frpv1alpha1.SchedulingPolicySpec{
			Budget: frpv1alpha1.BudgetSpec{MaxExits: maxExits},
			VPS: frpv1alpha1.VPSSpec{
				Default: frpv1alpha1.VPSDefaults{
					Provider: frpv1alpha1.ProviderDigitalOcean,
					Regions:  []string{"nyc1", "sfo3"},
					Size:     "s-1vcpu-1gb",
				},
			},
		},
	}
}

func TestOnDemand_ProvisionsWhenUnderBudget(t *testing.T) {
	p := &OnDemandStrategy{}
	max := int32(3)
	d, err := p.Plan(ProvisionInput{
		Tunnel:  basicTunnel(80),
		Policy:  basicPolicy(&max),
		Current: nil,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !d.Provision {
		t.Errorf("expected Provision=true; reason=%q", d.Reason)
	}
	if d.Spec.Provider != frpv1alpha1.ProviderDigitalOcean {
		t.Errorf("Spec.Provider=%q want digitalocean", d.Spec.Provider)
	}
	if d.Spec.Region != "nyc1" {
		t.Errorf("Spec.Region=%q want nyc1 (first in policy regions)", d.Spec.Region)
	}
	if d.Spec.Size != "s-1vcpu-1gb" {
		t.Errorf("Spec.Size=%q want s-1vcpu-1gb", d.Spec.Size)
	}
}

func TestOnDemand_RefusesWhenAtBudget(t *testing.T) {
	p := &OnDemandStrategy{}
	max := int32(2)
	current := []frpv1alpha1.ExitServer{
		{ObjectMeta: metav1.ObjectMeta{Name: "e1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e2"}},
	}
	d, _ := p.Plan(ProvisionInput{Tunnel: basicTunnel(80), Policy: basicPolicy(&max), Current: current})
	if d.Provision {
		t.Errorf("expected Provision=false; got Spec=%+v", d.Spec)
	}
	if d.Reason == "" {
		t.Error("expected non-empty Reason")
	}
}

func TestOnDemand_RefusesWhenNamespaceCapped(t *testing.T) {
	p := &OnDemandStrategy{}
	maxNs := int32(1)
	policy := basicPolicy(nil)
	policy.Spec.Budget.MaxExitsPerNamespace = &maxNs
	t1 := basicTunnel(80)
	t1.Namespace = "team-a"
	current := []frpv1alpha1.ExitServer{
		{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: "team-a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: "team-b"}},
	}
	d, _ := p.Plan(ProvisionInput{Tunnel: t1, Policy: policy, Current: current})
	if d.Provision {
		t.Errorf("expected Provision=false (per-ns budget hit); got Spec=%+v", d.Spec)
	}
}

func TestOnDemand_AppliesPlacementOverrides(t *testing.T) {
	p := &OnDemandStrategy{}
	max := int32(5)
	tunnel := basicTunnel(80)
	tunnel.Spec.Placement = &frpv1alpha1.Placement{
		Regions:      []string{"sfo3"},
		SizeOverride: "s-2vcpu-4gb",
	}
	d, err := p.Plan(ProvisionInput{Tunnel: tunnel, Policy: basicPolicy(&max), Current: nil})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !d.Provision {
		t.Fatalf("Provision=false; reason=%q", d.Reason)
	}
	if d.Spec.Region != "sfo3" {
		t.Errorf("Region=%q want sfo3", d.Spec.Region)
	}
	if d.Spec.Size != "s-2vcpu-4gb" {
		t.Errorf("Size=%q want s-2vcpu-4gb", d.Spec.Size)
	}
}

func TestFixedPool_RefusesBeyondPool(t *testing.T) {
	p := &FixedPoolStrategy{}
	max := int32(2)
	current := []frpv1alpha1.ExitServer{
		{ObjectMeta: metav1.ObjectMeta{Name: "e1"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "e2"}},
	}
	d, _ := p.Plan(ProvisionInput{Tunnel: basicTunnel(80), Policy: basicPolicy(&max), Current: current})
	if d.Provision {
		t.Error("FixedPool must refuse beyond MaxExits")
	}
}

func TestFixedPool_ProvisionsBelowPool(t *testing.T) {
	p := &FixedPoolStrategy{}
	max := int32(3)
	d, _ := p.Plan(ProvisionInput{Tunnel: basicTunnel(80), Policy: basicPolicy(&max), Current: nil})
	if !d.Provision {
		t.Error("FixedPool must provision below MaxExits")
	}
}

func TestProvisionStrategyNames(t *testing.T) {
	if (&OnDemandStrategy{}).Name() != "OnDemand" {
		t.Error()
	}
	if (&FixedPoolStrategy{}).Name() != "FixedPool" {
		t.Error()
	}
}
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/scheduler/ -run 'OnDemand|FixedPool|ProvisionStrategy' -v`
Expected: build fails.

- [ ] **Step 3: Write `internal/scheduler/provision_ondemand.go`**

```go
package scheduler

import (
	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// OnDemandStrategy provisions one new ExitServer when allocation fails,
// using the SchedulingPolicy.spec.vps.default settings. Tunnel-level
// placement preferences override the policy defaults.
type OnDemandStrategy struct{}

// Name implements ProvisionStrategy.
func (OnDemandStrategy) Name() string { return "OnDemand" }

// Plan implements ProvisionStrategy.
func (OnDemandStrategy) Plan(in ProvisionInput) (ProvisionDecision, error) {
	if reason := checkBudget(in); reason != "" {
		return ProvisionDecision{Reason: reason}, nil
	}
	return ProvisionDecision{Provision: true, Spec: composeSpec(in)}, nil
}

// checkBudget enforces the SchedulingPolicy.spec.budget caps. Returns a
// non-empty reason when the request must be refused; empty string when the
// budget allows another exit.
func checkBudget(in ProvisionInput) string {
	if in.Policy == nil {
		return ""
	}
	b := in.Policy.Spec.Budget
	if b.MaxExits != nil && int32(len(in.Current)) >= *b.MaxExits {
		return "BudgetExceeded: maxExits reached"
	}
	if b.MaxExitsPerNamespace != nil {
		ns := in.Tunnel.Namespace
		var count int32
		for _, e := range in.Current {
			if e.Namespace == ns {
				count++
			}
		}
		if count >= *b.MaxExitsPerNamespace {
			return "BudgetExceeded: maxExitsPerNamespace reached for " + ns
		}
	}
	return ""
}

// composeSpec produces the desired ExitServerSpec from the policy defaults
// and the tunnel's placement overrides.
func composeSpec(in ProvisionInput) frpv1alpha1.ExitServerSpec {
	def := in.Policy.Spec.VPS.Default
	spec := frpv1alpha1.ExitServerSpec{
		Provider: def.Provider,
		Region:   firstString(def.Regions),
		Size:     def.Size,
		Capacity: def.Capacity,
	}
	if in.Tunnel.Spec.Placement != nil {
		p := in.Tunnel.Spec.Placement
		if region := firstString(p.Regions); region != "" {
			spec.Region = region
		}
		if p.SizeOverride != "" {
			spec.Size = p.SizeOverride
		}
	}
	return spec
}

func firstString(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
```

- [ ] **Step 4: Write `internal/scheduler/provision_fixedpool.go`**

```go
package scheduler

// FixedPoolStrategy keeps the exit count at a fixed cap (Policy.spec.budget.maxExits).
// It NEVER provisions beyond that cap and DOES NOT pre-warm a pool — the
// "fixed" name reflects "never over the cap", not "always exactly N exits".
// Tunnels that arrive after the pool is full stay Pending until something
// frees up.
type FixedPoolStrategy struct{}

// Name implements ProvisionStrategy.
func (FixedPoolStrategy) Name() string { return "FixedPool" }

// Plan implements ProvisionStrategy. Identical to OnDemand under-budget,
// strict refusal otherwise.
func (FixedPoolStrategy) Plan(in ProvisionInput) (ProvisionDecision, error) {
	if reason := checkBudget(in); reason != "" {
		return ProvisionDecision{Reason: reason}, nil
	}
	return ProvisionDecision{Provision: true, Spec: composeSpec(in)}, nil
}
```

(Yes, these two files are nearly identical. The semantic split is intentional: future evolution may add pre-warming to `FixedPool` or quota-aware behaviors to `OnDemand`. Phase 4 keeps them as named, registered strategies the controller selects between.)

- [ ] **Step 5: Run, confirm PASS**

`devbox run -- go test ./internal/scheduler/ -v`
Expected: all tests pass (Tasks 1, 2, 3 combined).

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/provision_ondemand.go internal/scheduler/provision_fixedpool.go internal/scheduler/provision_test.go
git commit -m "feat(scheduler): OnDemand and FixedPool provision strategies"
```

---

## Phase 4 done — exit criteria

- `devbox run -- make test` passes (Phase 1 controllers + Phase 2 primitives + Phase 3 provider + Phase 4 scheduler).
- `internal/scheduler/` exposes:
  - Interfaces: `Allocator`, `ProvisionStrategy`.
  - Types: `AllocateInput`, `AllocationDecision`, `ProvisionInput`, `ProvisionDecision`, `ErrNotRegistered`.
  - Registries: `AllocatorRegistry`, `ProvisionStrategyRegistry`.
  - Built-in allocators: `BinPackAllocator`, `SpreadAllocator`, `CapacityAwareAllocator`.
  - Built-in strategies: `OnDemandStrategy`, `FixedPoolStrategy`.
  - Helpers: `EligibleExits`, `PortsFit`, `CapacityFits`, `PlacementMatches`.
- No controller, CRD, or provider code touched.

The next plan (Phase 5: ExitServerController) wires the controllers and is where `internal/provider`, `internal/scheduler`, `internal/frp/*`, and `internal/bootstrap` finally compose. Phase 5 also begins integration testing the full reconcile loops.
