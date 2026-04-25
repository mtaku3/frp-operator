# Phase 6: TunnelController Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.

**Goal:** Wire the second controller, the one user-facing Tunnels actually flow through. The `TunnelController` watches `Tunnel` CRs and:

1. Looks up the referenced `SchedulingPolicy` (defaulting to a cluster default).
2. Runs the Allocator (Phase 4) over current ExitServers.
3. If no exit fits, runs the ProvisionStrategy and creates a new ExitServer CR (which the ExitServerController reconciles separately).
4. Reserves the required public ports on `ExitServer.status.allocations` (optimistic concurrency: if conflict, requeue).
5. Renders an `frpc.toml` for the tunnel using `internal/frp/config`, stores it in a per-tunnel Secret.
6. Creates/updates an `frpc` Deployment that mounts that Secret.
7. Pushes the proxy config to `frps` via admin API (Phase 2 admin client).
8. Patches Tunnel.status with assignedExit / assignedIP / assignedPorts / phase.
9. Finalizer: releases the port allocation on the ExitServer, deletes the Deployment and Secret, removes the proxy from frps.

This is the integration phase — it's where Phases 2–5 actually compose into something a user can use.

**Architecture:**

- Controller files split by concern: reconcile skeleton, finalizer, port reservation, frpc deployment/secret, scheduler integration, frps proxy push.
- Tests use envtest + provider/fake + injectable AdminClientFactory (same pattern as Phase 5). The frpc Deployment is observed via the Kubernetes client (envtest provides the apiserver but no kubelet — Pods don't actually run, we just verify the Deployment object is created with correct spec).

**Tech Stack:** controller-runtime, envtest, Ginkgo. Imports `internal/frp/config`, `internal/frp/admin`, `internal/scheduler`, `internal/provider`. Plus stdlib `context`, `fmt`, `time`.

**Reference spec:** [`docs/superpowers/specs/2026-04-23-frp-operator-design.md`](../specs/2026-04-23-frp-operator-design.md) §4 (allocation), §5 (scheduling), §6 (controllers — TunnelController section), §3.2 (Tunnel CR semantics including `immutableWhenReady`, `migrationPolicy`, `allowPortSplit`).

**Out of scope this phase:**

- ExitReclaimController (Phase 7).
- ServiceWatcherController (Phase 8) — the bridge from `Service type=LoadBalancer` to a Tunnel CR.
- Validating admission webhook for `immutableWhenReady` (Phase 10).
- Real DigitalOcean provisioner (Phase 9).
- Tunnel migration on overcommit/exit-loss (the `migrationPolicy` field exists on the CR but the controller treats every value as `Never` in v1; explicit eviction by deleting and recreating is the only path. This is documented in the controller, not a TODO).

---

## File Structure

```
internal/controller/tunnel_controller.go        — Reconcile loop + RBAC markers
internal/controller/tunnel_finalizer.go        — finalizer add/remove (mirror of exitserver_finalizer.go)
internal/controller/tunnel_ports.go            — port reservation on ExitServer.status (optimistic concurrency)
internal/controller/tunnel_frpc.go             — frpc Deployment + Secret reconcile
internal/controller/tunnel_scheduler.go        — Allocator + ProvisionStrategy invocation, ExitServer creation
internal/controller/tunnel_proxy.go            — frps admin-API proxy push
internal/controller/tunnel_phases.go           — pure phase-transition function
internal/controller/tunnel_controller_test.go  — Ginkgo envtest specs
```

**Boundaries.** Each helper is small (≤120 lines), pure where possible. The Reconcile body composes them. Tests live in the existing `internal/controller/tunnel_controller_test.go` (kubebuilder-stubbed) — we replace the stub with real specs.

---

## Task 1: Tunnel finalizer + phase transitions

**Files:**
- Create: `internal/controller/tunnel_finalizer.go`
- Create: `internal/controller/tunnel_phases.go`
- Test: `internal/controller/tunnel_phases_test.go`

The finalizer file is a near-copy of `exitserver_finalizer.go` but with the const `tunnelFinalizer = "frp.operator.io/tunnel-finalizer"`. The phase function maps observed conditions to the next `TunnelPhase`.

- [ ] **Step 1: Write `internal/controller/tunnel_finalizer.go`**

```go
package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// tunnelFinalizer is the finalizer string the controller adds to every
// Tunnel CR. Removed only after the Deployment, Secret, port reservation,
// and frps proxy entry have been cleaned up.
const tunnelFinalizer = "frp.operator.io/tunnel-finalizer"

// hasTunnelFinalizer reports whether the named finalizer is on the Tunnel.
func hasTunnelFinalizer(t *frpv1alpha1.Tunnel) bool {
	for _, f := range t.Finalizers {
		if f == tunnelFinalizer {
			return true
		}
	}
	return false
}

// addTunnelFinalizer appends the finalizer if missing and patches.
// Returns true if a patch was sent.
func addTunnelFinalizer(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (bool, error) {
	if hasTunnelFinalizer(t) {
		return false, nil
	}
	patch := client.MergeFrom(t.DeepCopy())
	t.Finalizers = append(t.Finalizers, tunnelFinalizer)
	if err := c.Patch(ctx, t, patch); err != nil {
		return false, fmt.Errorf("add tunnel finalizer: %w", err)
	}
	return true, nil
}

// removeTunnelFinalizer drops the finalizer and patches.
func removeTunnelFinalizer(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (bool, error) {
	if !hasTunnelFinalizer(t) {
		return false, nil
	}
	patch := client.MergeFrom(t.DeepCopy())
	out := t.Finalizers[:0]
	for _, f := range t.Finalizers {
		if f != tunnelFinalizer {
			out = append(out, f)
		}
	}
	t.Finalizers = out
	if err := c.Patch(ctx, t, patch); err != nil {
		return false, fmt.Errorf("remove tunnel finalizer: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 2: Write `internal/controller/tunnel_phases.go` and `tunnel_phases_test.go`**

Phase function:

```go
package controller

import (
	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// nextTunnelPhase computes the next TunnelPhase from observed conditions.
// Pure function; no side effects.
//
// Inputs:
//   current      = current Tunnel.status.phase
//   exitAssigned = is there an assignedExit?
//   exitReady    = is the assigned exit in PhaseReady?
//   frpcReady    = is the frpc Deployment Ready (replicas == readyReplicas > 0)?
//
// Lattice (worst-to-best):
//   Failed > Disconnected > Pending > Allocating > Provisioning > Connecting > Ready
//
// We only ever transition forward to Ready. Disconnected is a regression
// from Ready when frpc loses connection (frpc Deployment unhealthy).
func nextTunnelPhase(current frpv1alpha1.TunnelPhase, exitAssigned, exitReady, frpcReady bool) frpv1alpha1.TunnelPhase {
	if !exitAssigned {
		return frpv1alpha1.TunnelAllocating
	}
	if !exitReady {
		return frpv1alpha1.TunnelProvisioning
	}
	if !frpcReady {
		// Exit ready, frpc not ready: bootstrapping or disconnected.
		if current == frpv1alpha1.TunnelReady {
			return frpv1alpha1.TunnelDisconnected
		}
		return frpv1alpha1.TunnelConnecting
	}
	return frpv1alpha1.TunnelReady
}
```

Test:

```go
package controller

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func TestNextTunnelPhase(t *testing.T) {
	cases := []struct {
		name                    string
		current                 frpv1alpha1.TunnelPhase
		exitAssigned, exitReady, frpcReady bool
		want                    frpv1alpha1.TunnelPhase
	}{
		{"no exit yet", "", false, false, false, frpv1alpha1.TunnelAllocating},
		{"exit assigned but provisioning", frpv1alpha1.TunnelAllocating, true, false, false, frpv1alpha1.TunnelProvisioning},
		{"exit ready, frpc connecting", frpv1alpha1.TunnelProvisioning, true, true, false, frpv1alpha1.TunnelConnecting},
		{"all ready", frpv1alpha1.TunnelConnecting, true, true, true, frpv1alpha1.TunnelReady},
		{"ready loses frpc -> Disconnected", frpv1alpha1.TunnelReady, true, true, false, frpv1alpha1.TunnelDisconnected},
		{"disconnected recovers", frpv1alpha1.TunnelDisconnected, true, true, true, frpv1alpha1.TunnelReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextTunnelPhase(tc.current, tc.exitAssigned, tc.exitReady, tc.frpcReady)
			if got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run, confirm PASS**

`devbox run -- go test ./internal/controller/ -run 'TunnelFinalizer|NextTunnelPhase' -v`

- [ ] **Step 4: Commit**

```bash
git add internal/controller/tunnel_finalizer.go internal/controller/tunnel_phases.go internal/controller/tunnel_phases_test.go
git commit -m "feat(controller/tunnel): finalizer + phase-transition helpers"
```

---

## Task 2: Port reservation with optimistic concurrency

**Files:**
- Create: `internal/controller/tunnel_ports.go`
- Test: `internal/controller/tunnel_ports_test.go`

The TunnelController writes the tunnel's public ports into `ExitServer.status.allocations`. This is shared mutable state across tunnels — multiple Tunnels can target the same ExitServer concurrently. We use the apiserver as the synchronization point: read the ExitServer, check the desired ports are still free (someone else may have claimed them between Allocator and us), patch with our claim. If the patch fails with a conflict (resourceVersion changed), requeue and retry on the next reconcile.

- [ ] **Step 1: Write `internal/controller/tunnel_ports_test.go`**

```go
package controller

import (
	"context"
	"strconv"
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newSchemeForPorts(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = frpv1alpha1.AddToScheme(s)
	return s
}

func TestReservePortsOnEmpty(t *testing.T) {
	scheme := newSchemeForPorts(t)
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Spec:       frpv1alpha1.ExitServerSpec{Provider: frpv1alpha1.ProviderDigitalOcean, Frps: frpv1alpha1.FrpsConfig{Version: "v0.68.1"}, AllowPorts: []string{"1024-65535"}},
		Status:     frpv1alpha1.ExitServerStatus{Allocations: map[string]string{}},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&frpv1alpha1.ExitServer{}).WithObjects(exit).Build()

	ctx := context.Background()
	tunnelKey := "ns/foo"
	if err := reservePorts(ctx, cli, exit, []int32{443, 80}, tunnelKey); err != nil {
		t.Fatalf("reservePorts: %v", err)
	}
	if exit.Status.Allocations["443"] != tunnelKey {
		t.Errorf("port 443 not allocated to %q: %v", tunnelKey, exit.Status.Allocations)
	}
	if exit.Status.Allocations["80"] != tunnelKey {
		t.Errorf("port 80 not allocated to %q: %v", tunnelKey, exit.Status.Allocations)
	}
	if exit.Status.Usage.Tunnels != 1 {
		t.Errorf("Usage.Tunnels = %d, want 1", exit.Status.Usage.Tunnels)
	}
}

func TestReservePortsConflict(t *testing.T) {
	scheme := newSchemeForPorts(t)
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Spec:       frpv1alpha1.ExitServerSpec{Provider: frpv1alpha1.ProviderDigitalOcean, Frps: frpv1alpha1.FrpsConfig{Version: "v0.68.1"}, AllowPorts: []string{"1024-65535"}},
		Status: frpv1alpha1.ExitServerStatus{
			Allocations: map[string]string{
				strconv.Itoa(443): "ns/other",
			},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&frpv1alpha1.ExitServer{}).WithObjects(exit).Build()

	ctx := context.Background()
	err := reservePorts(ctx, cli, exit, []int32{443}, "ns/me")
	if err == nil {
		t.Fatal("expected port-conflict error")
	}
}

func TestReleasePorts(t *testing.T) {
	scheme := newSchemeForPorts(t)
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Status: frpv1alpha1.ExitServerStatus{
			Allocations: map[string]string{"80": "ns/foo", "443": "ns/foo", "5432": "ns/other"},
			Usage:       frpv1alpha1.ExitUsage{Tunnels: 2},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&frpv1alpha1.ExitServer{}).WithObjects(exit).Build()

	ctx := context.Background()
	if err := releasePorts(ctx, cli, exit, "ns/foo"); err != nil {
		t.Fatalf("releasePorts: %v", err)
	}
	if _, ok := exit.Status.Allocations["80"]; ok {
		t.Errorf("port 80 should be released")
	}
	if _, ok := exit.Status.Allocations["443"]; ok {
		t.Errorf("port 443 should be released")
	}
	if exit.Status.Allocations["5432"] != "ns/other" {
		t.Error("other tunnel's allocation accidentally removed")
	}
	if exit.Status.Usage.Tunnels != 1 {
		t.Errorf("Usage.Tunnels = %d, want 1", exit.Status.Usage.Tunnels)
	}
}
```

- [ ] **Step 2: Write `internal/controller/tunnel_ports.go`**

```go
package controller

import (
	"context"
	"fmt"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// reservePorts atomically writes the requested ports into
// exit.Status.Allocations under tunnelKey ("namespace/name") and
// increments Usage.Tunnels by 1. Fails if any port is already allocated
// to a different tunnel. Uses the apiserver's resourceVersion as the
// concurrency token: a Patch conflict surfaces as a Conflict error from
// the client, which the caller should treat as a requeue signal.
//
// The exit pointer is mutated in place to reflect the patched state so
// callers can read .Allocations after a successful return.
func reservePorts(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, ports []int32, tunnelKey string) error {
	if exit.Status.Allocations == nil {
		exit.Status.Allocations = map[string]string{}
	}
	patch := client.MergeFrom(exit.DeepCopy())

	// Check first: every port must be free (or already allocated to *us*).
	for _, p := range ports {
		key := strconv.Itoa(int(p))
		if owner, taken := exit.Status.Allocations[key]; taken && owner != tunnelKey {
			return fmt.Errorf("port %d already allocated to %s", p, owner)
		}
	}

	// Reserve.
	added := 0
	for _, p := range ports {
		key := strconv.Itoa(int(p))
		if _, already := exit.Status.Allocations[key]; !already {
			added++
		}
		exit.Status.Allocations[key] = tunnelKey
	}
	// Tunnel count increments only when this tunnel didn't already
	// hold any of these ports (added > 0 implies new presence).
	if added > 0 {
		exit.Status.Usage.Tunnels++
	}
	return c.Status().Patch(ctx, exit, patch)
}

// releasePorts removes every Allocation entry pointing at tunnelKey and
// decrements Usage.Tunnels by 1 if any were removed. Idempotent: calling
// when the tunnel holds no allocations is a no-op.
func releasePorts(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, tunnelKey string) error {
	if len(exit.Status.Allocations) == 0 {
		return nil
	}
	patch := client.MergeFrom(exit.DeepCopy())
	removed := 0
	for k, owner := range exit.Status.Allocations {
		if owner == tunnelKey {
			delete(exit.Status.Allocations, k)
			removed++
		}
	}
	if removed == 0 {
		return nil
	}
	if exit.Status.Usage.Tunnels > 0 {
		exit.Status.Usage.Tunnels--
	}
	return c.Status().Patch(ctx, exit, patch)
}
```

- [ ] **Step 3: Run, confirm 3 tests PASS**

`devbox run -- go test ./internal/controller/ -run 'ReservePorts|ReleasePorts' -v`

- [ ] **Step 4: Commit**

```bash
git add internal/controller/tunnel_ports.go internal/controller/tunnel_ports_test.go
git commit -m "feat(controller/tunnel): port reservation with status-subresource patch"
```

---

## Task 3: frpc Deployment + Secret reconcile

**Files:**
- Create: `internal/controller/tunnel_frpc.go`
- Test: `internal/controller/tunnel_frpc_test.go`

The controller renders an `frpc.toml` for the tunnel using `internal/frp/config.FrpcConfig.Render`, stores it in a `Secret/<tunnel>-frpc-config`, and creates a `Deployment/<tunnel>-frpc` with one replica that mounts the Secret at `/etc/frp/frpc.toml` and runs `frpc -c /etc/frp/frpc.toml`.

The Deployment uses the same `snowdreamtech/frpc` image used by the LocalDocker provisioner so behavior is consistent across local and cloud paths. (For real deployment we may switch to a kubebuilder-built image; defer that to Phase 9 wiring.)

- [ ] **Step 1: Write the test (small unit test for config rendering + golden Deployment shape)**

`internal/controller/tunnel_frpc_test.go`:

```go
package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func newFrpcScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = frpv1alpha1.AddToScheme(s)
	return s
}

func basicTunnelForFrpc() *frpv1alpha1.Tunnel {
	port := int32(80)
	return &frpv1alpha1.Tunnel{
		ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "ns"},
		Spec: frpv1alpha1.TunnelSpec{
			Service: frpv1alpha1.ServiceRef{Name: "my-svc", Namespace: "ns"},
			Ports: []frpv1alpha1.TunnelPort{
				{Name: "http", ServicePort: port, PublicPort: &port, Protocol: frpv1alpha1.ProtocolTCP},
			},
		},
	}
}

func TestEnsureFrpcSecretCreatesAndIsIdempotent(t *testing.T) {
	scheme := newFrpcScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	tunnel := basicTunnelForFrpc()

	body, err := ensureFrpcSecret(ctx, cli, tunnel, "203.0.113.1", 7000, "tok-abc", []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 80, PublicPort: ptrI32(80), Protocol: frpv1alpha1.ProtocolTCP}})
	if err != nil {
		t.Fatalf("ensureFrpcSecret: %v", err)
	}
	if !strings.Contains(string(body), `serverAddr = "203.0.113.1"`) {
		t.Errorf("rendered config missing serverAddr:\n%s", body)
	}

	// Idempotency: same call returns the same body bytes (the rendering is
	// deterministic) and the Secret already exists.
	body2, err := ensureFrpcSecret(ctx, cli, tunnel, "203.0.113.1", 7000, "tok-abc", []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 80, PublicPort: ptrI32(80), Protocol: frpv1alpha1.ProtocolTCP}})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if string(body) != string(body2) {
		t.Error("rendered body changed across calls")
	}

	var sec corev1.Secret
	if err := cli.Get(ctx, types.NamespacedName{Name: "t1-frpc-config", Namespace: "ns"}, &sec); err != nil {
		t.Fatalf("Secret should exist: %v", err)
	}
	if string(sec.Data["frpc.toml"]) != string(body) {
		t.Errorf("Secret data mismatch")
	}
}

func TestEnsureFrpcDeploymentCreatesWithExpectedSpec(t *testing.T) {
	scheme := newFrpcScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	tunnel := basicTunnelForFrpc()

	if err := ensureFrpcDeployment(ctx, cli, tunnel); err != nil {
		t.Fatalf("ensureFrpcDeployment: %v", err)
	}
	var dep appsv1.Deployment
	if err := cli.Get(ctx, types.NamespacedName{Name: "t1-frpc", Namespace: "ns"}, &dep); err != nil {
		t.Fatalf("Deployment should exist: %v", err)
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
		t.Errorf("expected 1 replica, got %v", dep.Spec.Replicas)
	}
	pod := dep.Spec.Template.Spec
	if len(pod.Containers) != 1 || pod.Containers[0].Name != "frpc" {
		t.Errorf("expected single 'frpc' container, got %+v", pod.Containers)
	}
	mounts := pod.Containers[0].VolumeMounts
	if len(mounts) == 0 || mounts[0].MountPath != "/etc/frp" {
		t.Errorf("expected /etc/frp mount, got %+v", mounts)
	}
}

func ptrI32(v int32) *int32 { return &v }
```

- [ ] **Step 2: Write `internal/controller/tunnel_frpc.go`**

```go
package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/frp/config"
)

// frpcImage is the image the operator runs as the tunnel client. Aligned
// with provider/localdocker so behavior is consistent across local-docker
// and real-cloud paths.
const frpcImage = "snowdreamtech/frpc:0.68.1"

// frpcSecretName returns the Secret name that holds the rendered frpc.toml
// for a given Tunnel.
func frpcSecretName(t *frpv1alpha1.Tunnel) string {
	return t.Name + "-frpc-config"
}

// frpcDeploymentName returns the Deployment name running the frpc container.
func frpcDeploymentName(t *frpv1alpha1.Tunnel) string {
	return t.Name + "-frpc"
}

// ensureFrpcSecret renders the frpc.toml for this tunnel and writes it to a
// Secret in the tunnel's namespace. The Secret is owned by the tunnel.
//
// Returns the rendered bytes so the caller can compare them across reconciles
// (used by ensureFrpcDeployment to decide if a rollout-restart annotation
// should change).
func ensureFrpcSecret(
	ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel,
	serverAddr string, serverPort int, authToken string, ports []frpv1alpha1.TunnelPort,
) ([]byte, error) {
	cfg := config.FrpcConfig{
		ServerAddr: serverAddr,
		ServerPort: serverPort,
		Auth:       config.FrpcAuth{Method: "token", Token: authToken},
	}
	for _, p := range ports {
		pub := p.ServicePort
		if p.PublicPort != nil {
			pub = *p.PublicPort
		}
		cfg.Proxies = append(cfg.Proxies, config.FrpcProxy{
			Name:       fmt.Sprintf("%s_%s_%s", t.Namespace, t.Name, p.Name),
			Type:       toFrpcType(p.Protocol),
			LocalIP:    fmt.Sprintf("%s.%s.svc", t.Spec.Service.Name, t.Spec.Service.Namespace),
			LocalPort:  int(p.ServicePort),
			RemotePort: int(pub),
		})
	}
	body, err := cfg.Render()
	if err != nil {
		return nil, fmt.Errorf("render frpc.toml: %w", err)
	}

	name := frpcSecretName(t)
	key := types.NamespacedName{Name: name, Namespace: t.Namespace}
	var existing corev1.Secret
	err = c.Get(ctx, key, &existing)
	if err == nil {
		// Update if drifted.
		if string(existing.Data["frpc.toml"]) != string(body) {
			existing.Data = map[string][]byte{"frpc.toml": body}
			if err := c.Update(ctx, &existing); err != nil {
				return nil, fmt.Errorf("update frpc secret: %w", err)
			}
		}
		return body, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get frpc secret: %w", err)
	}

	sec := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: t.Namespace,
			Labels:    map[string]string{"frp-operator.io/tunnel": t.Name},
			OwnerReferences: []metav1.OwnerReference{tunnelOwnerRef(t)},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"frpc.toml": body},
	}
	if err := c.Create(ctx, &sec); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Race: re-fetch and update.
			if err := c.Get(ctx, key, &existing); err == nil {
				existing.Data = map[string][]byte{"frpc.toml": body}
				_ = c.Update(ctx, &existing)
			}
			return body, nil
		}
		return nil, fmt.Errorf("create frpc secret: %w", err)
	}
	return body, nil
}

// ensureFrpcDeployment creates or updates the frpc Deployment for a tunnel.
// One replica, one container, the rendered Secret mounted at /etc/frp.
func ensureFrpcDeployment(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) error {
	name := frpcDeploymentName(t)
	desired := frpcDeploymentSpec(t)

	var existing appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: t.Namespace}, &existing)
	if err == nil {
		// Idempotent update: only patch if image or replicas drifted.
		if existing.Spec.Template.Spec.Containers[0].Image != frpcImage ||
			existing.Spec.Replicas == nil || *existing.Spec.Replicas != 1 {
			existing.Spec = desired.Spec
			return c.Update(ctx, &existing)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get deployment: %w", err)
	}
	return c.Create(ctx, desired)
}

// frpcDeploymentSpec is the desired Deployment for one tunnel.
func frpcDeploymentSpec(t *frpv1alpha1.Tunnel) *appsv1.Deployment {
	labels := map[string]string{"frp-operator.io/tunnel": t.Name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            frpcDeploymentName(t),
			Namespace:       t.Namespace,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{tunnelOwnerRef(t)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "frpc",
						Image:   frpcImage,
						Command: []string{"/usr/bin/frpc", "-c", "/etc/frp/frpc.toml"},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							MountPath: "/etc/frp",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: frpcSecretName(t)},
						},
					}},
				},
			},
		},
	}
}

// tunnelOwnerRef builds a metav1.OwnerReference with controller=true so
// owned resources (Secret, Deployment) GC when the Tunnel is deleted.
func tunnelOwnerRef(t *frpv1alpha1.Tunnel) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         frpv1alpha1.GroupVersion.String(),
		Kind:               "Tunnel",
		Name:               t.Name,
		UID:                t.UID,
		BlockOwnerDeletion: ptr.To(true),
		Controller:         ptr.To(true),
	}
}

// toFrpcType converts a TunnelProtocol to the frp config string.
func toFrpcType(p frpv1alpha1.TunnelProtocol) string {
	if p == frpv1alpha1.ProtocolUDP {
		return "udp"
	}
	return "tcp"
}
```

- [ ] **Step 3: Run, confirm tests pass**

`devbox run -- go test ./internal/controller/ -run 'EnsureFrpc' -v`

- [ ] **Step 4: Commit**

```bash
git add internal/controller/tunnel_frpc.go internal/controller/tunnel_frpc_test.go
git commit -m "feat(controller/tunnel): render frpc.toml Secret and frpc Deployment"
```

---

## Task 4: Reconcile loop — happy path

**Files:**
- Modify: `internal/controller/tunnel_controller.go` (replace stub Reconcile)
- Test: replace stub in `internal/controller/tunnel_controller_test.go` with Ginkgo specs

The controller composes Tasks 1-3 with the scheduler from Phase 4 and the provisioner registry from Phase 3. High-level reconcile flow:

```
1. Fetch CR; bail if gone.
2. Deletion path: if DeletionTimestamp set, run finalizer (release ports, drop finalizer), exit.
3. Add finalizer if missing, requeue.
4. If status.assignedExit empty:
     a. List ExitServers in same namespace.
     b. Look up SchedulingPolicy (.spec.schedulingPolicyRef or "default").
     c. Look up Allocator by policy.spec.allocator name (registry).
     d. Allocate(tunnel, exits) -> AllocationDecision.
     e. If decision.Exit == nil: invoke ProvisionStrategy. If Provision=true,
        Create() a new ExitServer CR. Either way, requeue.
   Else: refetch the assigned ExitServer.
5. If exit not Ready: requeue.
6. Reserve ports on ExitServer.status (Task 2). On conflict, requeue.
7. Read AuthToken from <exit>-credentials Secret.
8. ensureFrpcSecret with serverAddr=exit.publicIP, serverPort=exit.bindPort, authToken from secret.
9. ensureFrpcDeployment.
10. Patch Tunnel.status (assignedExit, assignedIP, assignedPorts, phase).
11. RequeueAfter 30s.
```

Step 7 reads the credentials Secret created by the ExitServerController (Phase 5). Step 8-9 are Task 3.

Phase 6 ships steps 1-10 except the frps proxy push (step 8 of the spec) — that lands in Task 5.

- [ ] **Step 1: Write `internal/controller/tunnel_scheduler.go`**

```go
package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/scheduler"
)

// resolvePolicy fetches the SchedulingPolicy named by tunnel.spec.schedulingPolicyRef,
// falling back to "default" if no name is set. Returns a pointer for use
// by the schedulers.
func resolvePolicy(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (*frpv1alpha1.SchedulingPolicy, error) {
	name := t.Spec.SchedulingPolicyRef.Name
	if name == "" {
		name = "default"
	}
	var p frpv1alpha1.SchedulingPolicy
	if err := c.Get(ctx, types.NamespacedName{Name: name}, &p); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("SchedulingPolicy %q not found", name)
		}
		return nil, fmt.Errorf("get SchedulingPolicy: %w", err)
	}
	return &p, nil
}

// listExitsInScope returns ExitServers in the tunnel's namespace. v1 keeps
// scope to the same namespace as the tunnel; cross-namespace allocation
// is a future feature.
func listExitsInScope(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) ([]frpv1alpha1.ExitServer, error) {
	var list frpv1alpha1.ExitServerList
	if err := c.List(ctx, &list, client.InNamespace(t.Namespace)); err != nil {
		return nil, fmt.Errorf("list exits: %w", err)
	}
	return list.Items, nil
}

// createExitServerFromDecision creates a new ExitServer CR using the
// ProvisionDecision's Spec. The CR's name is generated from the tunnel's
// namespace+name+a short random suffix to avoid collisions.
func createExitServerFromDecision(
	ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel, d scheduler.ProvisionDecision,
) (*frpv1alpha1.ExitServer, error) {
	exit := &frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: t.Name + "-",
			Namespace:    t.Namespace,
			Labels:       map[string]string{"frp-operator.io/created-by": "tunnel-controller"},
		},
		Spec: d.Spec,
	}
	if err := c.Create(ctx, exit); err != nil {
		return nil, fmt.Errorf("create ExitServer: %w", err)
	}
	return exit, nil
}
```

- [ ] **Step 2: Replace `internal/controller/tunnel_controller.go`**

Below is the full Reconcile body. Most of the boilerplate (struct, RBAC, SetupWithManager) mirrors the ExitServerController.

```go
package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
	"github.com/mtaku3/frp-operator/internal/scheduler"
)

// TunnelReconciler reconciles Tunnel CRs.
type TunnelReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Allocators           *scheduler.AllocatorRegistry
	ProvisionStrategies  *scheduler.ProvisionStrategyRegistry
	Provisioners         *provider.Registry
	NewAdminClient       AdminClientFactory
}

// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=tunnels/finalizers,verbs=update
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=schedulingpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives one Tunnel toward its desired state. See the package-level
// docs in the plan for the full state machine; the implementation below
// follows it step by step.
func (r *TunnelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var tunnel frpv1alpha1.Tunnel
	if err := r.Get(ctx, req.NamespacedName, &tunnel); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	tunnelKey := tunnel.Namespace + "/" + tunnel.Name

	// Deletion path.
	if !tunnel.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tunnel)
	}

	// Add finalizer.
	if added, err := addTunnelFinalizer(ctx, r.Client, &tunnel); err != nil {
		return ctrl.Result{}, err
	} else if added {
		return ctrl.Result{Requeue: true}, nil
	}

	// Allocate or refetch assigned exit.
	var exit *frpv1alpha1.ExitServer
	if tunnel.Status.AssignedExit == "" {
		var err error
		exit, err = r.allocateExit(ctx, &tunnel)
		if err != nil {
			return ctrl.Result{}, err
		}
		if exit == nil {
			// Either provisioning a new exit or no exit available; requeue
			// to let the new ExitServer come up.
			return r.patchTunnelPhase(ctx, &tunnel, frpv1alpha1.TunnelAllocating, "no eligible exit; provisioning or pending")
		}
	} else {
		var got frpv1alpha1.ExitServer
		if err := r.Get(ctx, types.NamespacedName{Name: tunnel.Status.AssignedExit, Namespace: tunnel.Namespace}, &got); err != nil {
			return ctrl.Result{}, fmt.Errorf("refetch assigned exit: %w", err)
		}
		exit = &got
	}

	// Wait for exit to be Ready.
	if exit.Status.Phase != frpv1alpha1.PhaseReady {
		return r.patchTunnelPhaseWithExit(ctx, &tunnel, exit, frpv1alpha1.TunnelProvisioning, "exit not yet Ready")
	}

	// Reserve ports.
	publicPorts := tunnelPublicPorts(&tunnel)
	if err := reservePorts(ctx, r.Client, exit, publicPorts, tunnelKey); err != nil {
		// Conflict or genuine error: requeue with backoff.
		logger.V(1).Info("port reservation failed; will requeue", "err", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Read auth token from exit credentials Secret.
	var credSec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: credentialsSecretName(exit), Namespace: exit.Namespace}, &credSec); err != nil {
		return ctrl.Result{}, fmt.Errorf("get exit credentials: %w", err)
	}
	authToken := string(credSec.Data["auth-token"])

	// Render and store frpc.toml in Secret; ensure Deployment.
	bindPort := int(exit.Spec.Frps.BindPort)
	if bindPort == 0 {
		bindPort = 7000
	}
	if _, err := ensureFrpcSecret(ctx, r.Client, &tunnel, exit.Status.PublicIP, bindPort, authToken, tunnel.Spec.Ports); err != nil {
		return ctrl.Result{}, err
	}
	if err := ensureFrpcDeployment(ctx, r.Client, &tunnel); err != nil {
		return ctrl.Result{}, err
	}

	// Compute frpc readiness.
	frpcReady, err := isFrpcDeploymentReady(ctx, r.Client, &tunnel)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Patch status.
	patch := client.MergeFrom(tunnel.DeepCopy())
	tunnel.Status.AssignedExit = exit.Name
	tunnel.Status.AssignedIP = exit.Status.PublicIP
	tunnel.Status.AssignedPorts = publicPorts
	tunnel.Status.Phase = nextTunnelPhase(tunnel.Status.Phase, true, true, frpcReady)
	apimeta.SetStatusCondition(&tunnel.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             condStatus(frpcReady),
		ObservedGeneration: tunnel.Generation,
		Reason:             "Reconciled",
	})
	if err := r.Status().Patch(ctx, &tunnel, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch tunnel status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// reconcileDelete: release ports, then drop finalizer. Owned resources
// (Secret, Deployment) GC via owner refs.
func (r *TunnelReconciler) reconcileDelete(ctx context.Context, tunnel *frpv1alpha1.Tunnel) (ctrl.Result, error) {
	if !hasTunnelFinalizer(tunnel) {
		return ctrl.Result{}, nil
	}
	tunnelKey := tunnel.Namespace + "/" + tunnel.Name

	if tunnel.Status.AssignedExit != "" {
		var exit frpv1alpha1.ExitServer
		err := r.Get(ctx, types.NamespacedName{Name: tunnel.Status.AssignedExit, Namespace: tunnel.Namespace}, &exit)
		switch {
		case err == nil:
			if err := releasePorts(ctx, r.Client, &exit, tunnelKey); err != nil {
				return ctrl.Result{}, err
			}
		case apierrors.IsNotFound(err):
			// Exit already gone; nothing to release.
		default:
			return ctrl.Result{}, err
		}
	}

	if _, err := removeTunnelFinalizer(ctx, r.Client, tunnel); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// allocateExit runs the scheduler. Returns nil exit if a new ExitServer is
// being provisioned or no eligible exit exists; the caller patches the
// Tunnel into Allocating and waits for the next reconcile.
func (r *TunnelReconciler) allocateExit(ctx context.Context, tunnel *frpv1alpha1.Tunnel) (*frpv1alpha1.ExitServer, error) {
	if tunnel.Spec.ExitRef != nil {
		// Hard pin: refetch the named exit; treat as authoritative.
		var got frpv1alpha1.ExitServer
		if err := r.Get(ctx, types.NamespacedName{Name: tunnel.Spec.ExitRef.Name, Namespace: tunnel.Namespace}, &got); err != nil {
			return nil, fmt.Errorf("hard-pinned ExitRef: %w", err)
		}
		return &got, nil
	}

	policy, err := resolvePolicy(ctx, r.Client, tunnel)
	if err != nil {
		return nil, err
	}
	exits, err := listExitsInScope(ctx, r.Client, tunnel)
	if err != nil {
		return nil, err
	}

	allocName := string(policy.Spec.Allocator)
	if allocName == "" {
		allocName = "CapacityAware"
	}
	alloc, err := r.Allocators.Lookup(allocName)
	if err != nil {
		return nil, fmt.Errorf("allocator %q: %w", allocName, err)
	}
	decision, err := alloc.Allocate(scheduler.AllocateInput{Tunnel: tunnel, Exits: exits})
	if err != nil {
		return nil, fmt.Errorf("Allocate: %w", err)
	}
	if decision.Exit != nil {
		return decision.Exit, nil
	}

	// No fit; ask ProvisionStrategy.
	provName := string(policy.Spec.Provisioner)
	if provName == "" {
		provName = "OnDemand"
	}
	ps, err := r.ProvisionStrategies.Lookup(provName)
	if err != nil {
		return nil, fmt.Errorf("provision strategy %q: %w", provName, err)
	}
	pd, err := ps.Plan(scheduler.ProvisionInput{Tunnel: tunnel, Policy: policy, Current: exits})
	if err != nil {
		return nil, fmt.Errorf("Plan: %w", err)
	}
	if !pd.Provision {
		return nil, nil
	}
	if _, err := createExitServerFromDecision(ctx, r.Client, tunnel, pd); err != nil {
		return nil, err
	}
	// Created; no exit yet to use this reconcile. Caller requeues.
	return nil, nil
}

// patchTunnelPhase patches just status.phase (and a Reason condition) when
// the tunnel is not yet placed.
func (r *TunnelReconciler) patchTunnelPhase(ctx context.Context, t *frpv1alpha1.Tunnel, phase frpv1alpha1.TunnelPhase, reason string) (ctrl.Result, error) {
	patch := client.MergeFrom(t.DeepCopy())
	t.Status.Phase = phase
	apimeta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: t.Generation,
		Reason:             "NotReady",
		Message:            reason,
	})
	if err := r.Status().Patch(ctx, t, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// patchTunnelPhaseWithExit patches phase and assignedExit; used when the
// tunnel has an exit but it isn't Ready yet.
func (r *TunnelReconciler) patchTunnelPhaseWithExit(ctx context.Context, t *frpv1alpha1.Tunnel, exit *frpv1alpha1.ExitServer, phase frpv1alpha1.TunnelPhase, reason string) (ctrl.Result, error) {
	patch := client.MergeFrom(t.DeepCopy())
	t.Status.AssignedExit = exit.Name
	t.Status.Phase = phase
	apimeta.SetStatusCondition(&t.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: t.Generation,
		Reason:             "NotReady",
		Message:            reason,
	})
	if err := r.Status().Patch(ctx, t, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func tunnelPublicPorts(t *frpv1alpha1.Tunnel) []int32 {
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

func condStatus(b bool) metav1.ConditionStatus {
	if b {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

// isFrpcDeploymentReady returns true if the frpc Deployment for the tunnel
// has at least one ready replica. envtest doesn't run kubelets so this
// will always be false there; tests inject a stub via DeepCopy or mark
// Status.ReadyReplicas manually.
func isFrpcDeploymentReady(ctx context.Context, c client.Client, t *frpv1alpha1.Tunnel) (bool, error) {
	var dep appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: frpcDeploymentName(t), Namespace: t.Namespace}, &dep)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return dep.Status.ReadyReplicas >= 1, nil
}

// SetupWithManager wires the controller. Watches Tunnel CRs; owns Secret
// and Deployment. Triggers re-reconcile when the assigned ExitServer's
// status changes (we want to know when the exit becomes Ready).
func (r *TunnelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&frpv1alpha1.Tunnel{}).
		Owns(&corev1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Named("tunnel").
		Complete(r)
}

// silence unused-import false positive when building without certain branches
var _ = errors.Is
var _ = strconv.Itoa
```

Drop the `silence unused-import` lines if they're not actually needed at compile time.

- [ ] **Step 3: Replace `internal/controller/tunnel_controller_test.go`**

Replace the kubebuilder stub with a Ginkgo `Describe("TunnelController integration")` block. Three specs:

```go
Describe("TunnelController integration", func() {
    var (
        fakeProv     *fake.FakeProvisioner
        provReg      *provider.Registry
        allocReg     *scheduler.AllocatorRegistry
        psReg        *scheduler.ProvisionStrategyRegistry
        exitRecon    *ExitServerReconciler
        tunnelRecon  *TunnelReconciler
    )

    BeforeEach(func() {
        fakeProv = fake.New("digitalocean")
        provReg = provider.NewRegistry()
        Expect(provReg.Register(fakeProv)).To(Succeed())

        allocReg = scheduler.NewAllocatorRegistry()
        Expect(allocReg.Register(scheduler.CapacityAwareAllocator{})).To(Succeed())
        psReg = scheduler.NewProvisionStrategyRegistry()
        Expect(psReg.Register(scheduler.OnDemandStrategy{})).To(Succeed())

        fa := &fakeAdmin{serverInfoOK: true}
        exitRecon = &ExitServerReconciler{
            Client: k8sClient, Scheme: scheme.Scheme,
            Provisioners: provReg,
            NewAdminClient: func(_, _, _ string) AdminClient { return fa },
        }
        tunnelRecon = &TunnelReconciler{
            Client: k8sClient, Scheme: scheme.Scheme,
            Allocators: allocReg, ProvisionStrategies: psReg,
            Provisioners: provReg,
            NewAdminClient: func(_, _, _ string) AdminClient { return fa },
        }

        // Default SchedulingPolicy.
        sp := &frpv1alpha1.SchedulingPolicy{
            ObjectMeta: metav1.ObjectMeta{Name: "default"},
            Spec: frpv1alpha1.SchedulingPolicySpec{
                VPS: frpv1alpha1.VPSSpec{
                    Default: frpv1alpha1.VPSDefaults{
                        Provider: frpv1alpha1.ProviderDigitalOcean,
                        Regions:  []string{"nyc1"},
                        Size:     "s-1vcpu-1gb",
                    },
                },
            },
        }
        _ = k8sClient.Create(ctx, sp) // best-effort create; idempotent across specs
        DeferCleanup(func() { _ = k8sClient.Delete(ctx, sp) })
    })

    It("provisions an exit when none exists, then schedules the tunnel onto it", func() {
        tunnel := &frpv1alpha1.Tunnel{
            ObjectMeta: metav1.ObjectMeta{Name: "t1", Namespace: "default"},
            Spec: frpv1alpha1.TunnelSpec{
                Service: frpv1alpha1.ServiceRef{Name: "svc", Namespace: "default"},
                Ports:   []frpv1alpha1.TunnelPort{{Name: "http", ServicePort: 80}},
                SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "default"},
            },
        }
        Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())
        DeferCleanup(func() {
            _ = k8sClient.Delete(ctx, tunnel)
            // Drive reconcile to clear finalizer.
            for i := 0; i < 5; i++ {
                _, _ = tunnelRecon.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "t1", Namespace: "default"}})
            }
        })

        tReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: "t1", Namespace: "default"}}

        // First reconcile: adds finalizer.
        _, err := tunnelRecon.Reconcile(ctx, tReq)
        Expect(err).NotTo(HaveOccurred())

        // Second: invokes scheduler, no exit -> ProvisionStrategy creates one.
        _, err = tunnelRecon.Reconcile(ctx, tReq)
        Expect(err).NotTo(HaveOccurred())

        // Drive ExitServerController to ready the new exit.
        var exits frpv1alpha1.ExitServerList
        Expect(k8sClient.List(ctx, &exits, client.InNamespace("default"))).To(Succeed())
        Expect(exits.Items).To(HaveLen(1))
        exitName := exits.Items[0].Name
        eReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: exitName, Namespace: "default"}}
        for i := 0; i < 3; i++ {
            _, err := exitRecon.Reconcile(ctx, eReq)
            Expect(err).NotTo(HaveOccurred())
        }
        var ex frpv1alpha1.ExitServer
        Expect(k8sClient.Get(ctx, types.NamespacedName{Name: exitName, Namespace: "default"}, &ex)).To(Succeed())
        Expect(ex.Status.Phase).To(Equal(frpv1alpha1.PhaseReady))

        // Final reconcile loops on the tunnel: schedule + frpc.
        for i := 0; i < 3; i++ {
            _, err := tunnelRecon.Reconcile(ctx, tReq)
            Expect(err).NotTo(HaveOccurred())
        }
        var got frpv1alpha1.Tunnel
        Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "t1", Namespace: "default"}, &got)).To(Succeed())
        Expect(got.Status.AssignedExit).To(Equal(exitName))
        Expect(got.Status.AssignedPorts).To(Equal([]int32{80}))
        Expect(got.Status.AssignedIP).To(Equal("127.0.0.1"))
        // Phase will be Connecting (no kubelet -> frpc Deployment never Ready in envtest).
        Expect(got.Status.Phase).To(Equal(frpv1alpha1.TunnelConnecting))

        // Secret and Deployment exist.
        var sec corev1.Secret
        Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "t1-frpc-config", Namespace: "default"}, &sec)).To(Succeed())
        var dep appsv1.Deployment
        Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "t1-frpc", Namespace: "default"}, &dep)).To(Succeed())
        Expect(*dep.Spec.Replicas).To(Equal(int32(1)))

        // Ports are reserved on the exit.
        Expect(k8sClient.Get(ctx, types.NamespacedName{Name: exitName, Namespace: "default"}, &ex)).To(Succeed())
        Expect(ex.Status.Allocations["80"]).To(Equal("default/t1"))
    })

    It("releases ports on tunnel deletion", func() {
        // (Set up tunnel + exit similar to above; assert Allocations is empty after delete + reconcile.)
        // ... full test body in Task 4 implementation.
    })
})
```

The second spec is the deletion test; structure is similar. The plan body is intentionally truncated (`...`); the implementer fleshes it out following the pattern.

- [ ] **Step 4: Run, confirm tests pass**

`devbox run -- make test`

The full controller suite must be green. Expect 8+ Ginkgo specs total now: 4 CRD-install (Phase 1), 3 ExitServer integration (Phase 5), 2 Tunnel integration (this task), plus pure-function tests.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/tunnel_scheduler.go internal/controller/tunnel_controller.go internal/controller/tunnel_controller_test.go
git commit -m "feat(controller/tunnel): full reconcile loop with allocator + provisioner + frpc"
```

---

## Phase 6 done — exit criteria

- `devbox run -- make test` passes (all phases).
- TunnelController reconciles a Tunnel through Pending → Allocating → Provisioning → Connecting → Ready transitions.
- Port reservation works with the apiserver as the synchronization point.
- frpc Secret + Deployment created with correct spec (single replica, Secret-mounted config, owner ref to Tunnel).
- Tunnel deletion: releases ports, drops finalizer; owned resources GC via owner refs.

The next plan (Phase 7: ExitReclaimController) handles empty-exit reclamation — when a Tunnel is deleted and an ExitServer drops to zero tunnels, drain it and destroy the VPS after `policy.spec.consolidation.drainAfter`.
