# Phase 5: ExitServerController Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.

**Goal:** Wire the first real controller. The `ExitServerController` watches `ExitServer` CRs and drives one VPS through its lifecycle: provision via `Provisioner` → bootstrap via cloud-init → reconcile `frps` config via admin API → probe health → tear down on delete.

This is the first phase that actually composes Phase 2-4 primitives. Reconcile correctness is checked against envtest + `provider/fake`.

**Architecture:**

- `ExitServerController.Reconcile` is event-driven: it computes desired state from `Spec`, observes actual state via `Provisioner.Inspect` + `admin.Client.ServerInfo`, and patches `Status` until they converge.
- Provisioner selection: by `spec.provider`, looked up in `internal/provider.Registry` populated at startup.
- Per-exit secret: the controller stores the generated frps admin token + auth token in a `Secret` named `<exit>-credentials` in the same namespace.
- Finalizer: `frp.operator.io/exitserver-finalizer`. On delete, the controller calls `Provisioner.Destroy`, removes the Secret, then drops the finalizer.
- Periodic health requeue: every `policy.spec.probes.adminInterval` and `providerInterval` (each independent timer).

**Tech Stack:** controller-runtime, envtest, Ginkgo (kubebuilder default for the controller suite). Stdlib for non-controller helpers.

**Reference spec:** [`docs/superpowers/specs/2026-04-23-frp-operator-design.md`](../specs/2026-04-23-frp-operator-design.md) §6 (controllers), §7 (bootstrap and auth), §8 (failure handling).

**Out of scope:** TunnelController (Phase 6), ExitReclaimController (Phase 7), ServiceWatcherController (Phase 8), real DigitalOcean provisioner (Phase 9), validating webhook (Phase 10), e2e harness (Phase 11). LocalDocker provisioner is available but unit tests prefer Fake to avoid Docker dependency.

---

## File Structure

```
internal/controller/exitserver_controller.go     — Reconcile loop + RBAC markers
internal/controller/exitserver_finalizer.go     — finalizer add/remove logic
internal/controller/exitserver_secrets.go       — token generation + Secret reconcile
internal/controller/exitserver_admin.go         — admin-API client wiring (resolves admin URL/creds)
internal/controller/exitserver_phases.go        — phase transition logic (Pending → Provisioning → Ready → ...)
internal/controller/exitserver_controller_test.go — Ginkgo envtest specs (replaces stub)
internal/controller/testdata/                   — golden frps config snippets, if any
```

**Boundaries.** Controller files split by concern (finalizer / secrets / admin / phases / Reconcile). Each ≤ 200 lines so the reconcile loop is readable. Tests use `provider/fake` plus an in-process httptest stand-in for the admin API (we don't need a real `frps` for controller unit tests).

---

## Task 1: Token generation and Secret reconcile

**Files:**
- Create: `internal/controller/exitserver_secrets.go`
- Test: `internal/controller/exitserver_secrets_test.go`

The controller generates two random tokens per exit:
- `admin-password` — used by the operator to authenticate to `frps`'s webServer admin API.
- `auth-token` — used by `frpc` clients connecting to this exit's `frps`.

Both live in `Secret/<exit-name>-credentials` in the ExitServer's namespace, keyed by `admin-password` and `auth-token` respectively. The controller creates the Secret on first reconcile, doesn't rotate (rotation = delete the Secret and let it regenerate, deferred to a later phase).

- [ ] **Step 1: Write the failing test**

`internal/controller/exitserver_secrets_test.go`:

```go
package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func newSchemeForTest(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = frpv1alpha1.AddToScheme(s)
	return s
}

func TestEnsureCredentialsSecretCreatesNewSecret(t *testing.T) {
	scheme := newSchemeForTest(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	exit := &frpv1alpha1.ExitServer{}
	exit.Name = "exit-1"
	exit.Namespace = "default"

	got, err := ensureCredentialsSecret(ctx, cli, exit)
	if err != nil {
		t.Fatalf("ensureCredentialsSecret: %v", err)
	}
	if got.AdminPassword == "" || len(got.AdminPassword) < 32 {
		t.Errorf("AdminPassword too short: %d chars", len(got.AdminPassword))
	}
	if got.AuthToken == "" || len(got.AuthToken) < 32 {
		t.Errorf("AuthToken too short: %d chars", len(got.AuthToken))
	}

	// Secret should now exist in the cluster.
	var sec corev1.Secret
	if err := cli.Get(ctx, types.NamespacedName{Name: "exit-1-credentials", Namespace: "default"}, &sec); err != nil {
		t.Fatalf("Get secret: %v", err)
	}
	if string(sec.Data["admin-password"]) != got.AdminPassword {
		t.Error("Secret admin-password mismatch")
	}
	if string(sec.Data["auth-token"]) != got.AuthToken {
		t.Error("Secret auth-token mismatch")
	}
	if sec.Labels["frp-operator.io/exit"] != "exit-1" {
		t.Errorf("expected label frp-operator.io/exit=exit-1, got %q", sec.Labels["frp-operator.io/exit"])
	}
}

func TestEnsureCredentialsSecretIsIdempotent(t *testing.T) {
	scheme := newSchemeForTest(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()
	exit := &frpv1alpha1.ExitServer{}
	exit.Name = "exit-1"
	exit.Namespace = "default"

	first, err := ensureCredentialsSecret(ctx, cli, exit)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := ensureCredentialsSecret(ctx, cli, exit)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first.AdminPassword != second.AdminPassword {
		t.Error("AdminPassword changed across calls")
	}
	if first.AuthToken != second.AuthToken {
		t.Error("AuthToken changed across calls")
	}
}
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/controller/ -run TestEnsureCredentials -v`

- [ ] **Step 3: Implement**

`internal/controller/exitserver_secrets.go`:

```go
package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// Credentials holds the random secrets the operator generates for one
// ExitServer. They are NOT user-supplied; the controller writes them on
// first reconcile and reads them on subsequent reconciles via the Secret
// it created.
type Credentials struct {
	AdminPassword string
	AuthToken     string
}

// credentialsSecretName returns "<exit-name>-credentials".
func credentialsSecretName(exit *frpv1alpha1.ExitServer) string {
	return exit.Name + "-credentials"
}

// ensureCredentialsSecret returns the Credentials for the given ExitServer,
// creating the backing Secret with fresh random values if it doesn't yet
// exist. The Secret is owned by the ExitServer (via OwnerReferences) so
// it's garbage-collected when the ExitServer is deleted.
//
// On subsequent calls the existing Secret is read and its values returned
// unchanged — never rotated implicitly.
func ensureCredentialsSecret(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer) (Credentials, error) {
	name := credentialsSecretName(exit)
	key := types.NamespacedName{Name: name, Namespace: exit.Namespace}

	var sec corev1.Secret
	err := c.Get(ctx, key, &sec)
	if err == nil {
		return Credentials{
			AdminPassword: string(sec.Data["admin-password"]),
			AuthToken:     string(sec.Data["auth-token"]),
		}, nil
	}
	if !apierrors.IsNotFound(err) {
		return Credentials{}, fmt.Errorf("get secret %s: %w", key, err)
	}

	creds := Credentials{
		AdminPassword: randomHex(32),
		AuthToken:     randomHex(32),
	}
	sec = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: exit.Namespace,
			Labels: map[string]string{
				"frp-operator.io/exit": exit.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				*ctrl.OwnerReference(exit, exit.GroupVersionKind()),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"admin-password": []byte(creds.AdminPassword),
			"auth-token":     []byte(creds.AuthToken),
		},
	}
	if err := c.Create(ctx, &sec); err != nil {
		// Race: another reconcile created it concurrently. Re-read.
		if apierrors.IsAlreadyExists(err) {
			if err := c.Get(ctx, key, &sec); err == nil {
				return Credentials{
					AdminPassword: string(sec.Data["admin-password"]),
					AuthToken:     string(sec.Data["auth-token"]),
				}, nil
			}
		}
		return Credentials{}, fmt.Errorf("create secret %s: %w", key, err)
	}
	return creds, nil
}

// randomHex returns 2*n hex characters of crypto-random data.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("controller: rand: %v", err))
	}
	return hex.EncodeToString(b)
}
```

**Note on `ctrl.OwnerReference`**: The package may not export an `OwnerReference` helper directly; if not, build the OwnerReference manually:

```go
ownerRef := metav1.OwnerReference{
    APIVersion:         exit.APIVersion,
    Kind:               exit.Kind,
    Name:               exit.Name,
    UID:                exit.UID,
    BlockOwnerDeletion: ptr.To(true),
    Controller:         ptr.To(true),
}
```

…and `ObjectMeta.GroupVersionKind()` may need the Scheme to populate `APIVersion`/`Kind`. If those fields are empty on a freshly-constructed CR, set them manually:

```go
exit.APIVersion = frpv1alpha1.GroupVersion.String()
exit.Kind = "ExitServer"
```

The test `TestEnsureCredentialsSecretCreatesNewSecret` constructs the ExitServer in-test, so populating these in the test helper is fine. Real reconcile receives ExitServers from the apiserver (TypeMeta is populated by controller-runtime).

- [ ] **Step 4: Run, confirm PASS**

`devbox run -- go test ./internal/controller/ -run TestEnsureCredentials -v` (with `dangerouslyDisableSandbox: true`)

- [ ] **Step 5: Commit**

```bash
git add internal/controller/exitserver_secrets.go internal/controller/exitserver_secrets_test.go
git commit -m "feat(controller): per-ExitServer credentials Secret reconcile"
```

---

## Task 2: Phase transition logic

**Files:**
- Create: `internal/controller/exitserver_phases.go`
- Test: `internal/controller/exitserver_phases_test.go`

A pure function that maps the observed `provider.State` (and the controller's view of frps health) to the next `ExitPhase` to write into `Status`. Pure function: testable in isolation, no client calls.

- [ ] **Step 1: Write the failing test**

`internal/controller/exitserver_phases_test.go`:

```go
package controller

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
)

func TestNextPhase(t *testing.T) {
	type input struct {
		current     frpv1alpha1.ExitPhase
		providerSt  provider.Phase
		adminOK     bool // last admin-API probe outcome
	}
	cases := []struct {
		name string
		in   input
		want frpv1alpha1.ExitPhase
	}{
		{
			name: "fresh CR with no provider state yet",
			in:   input{current: "", providerSt: "", adminOK: false},
			want: frpv1alpha1.PhasePending,
		},
		{
			name: "provider says provisioning",
			in:   input{current: frpv1alpha1.PhasePending, providerSt: provider.PhaseProvisioning, adminOK: false},
			want: frpv1alpha1.PhaseProvisioning,
		},
		{
			name: "provider running, admin not yet OK",
			in:   input{current: frpv1alpha1.PhaseProvisioning, providerSt: provider.PhaseRunning, adminOK: false},
			want: frpv1alpha1.PhaseProvisioning, // still bootstrapping; admin not up
		},
		{
			name: "provider running and admin OK -> Ready",
			in:   input{current: frpv1alpha1.PhaseProvisioning, providerSt: provider.PhaseRunning, adminOK: true},
			want: frpv1alpha1.PhaseReady,
		},
		{
			name: "ready exit fails admin probe -> Degraded",
			in:   input{current: frpv1alpha1.PhaseReady, providerSt: provider.PhaseRunning, adminOK: false},
			want: frpv1alpha1.PhaseDegraded,
		},
		{
			name: "degraded exit recovers admin -> Ready",
			in:   input{current: frpv1alpha1.PhaseDegraded, providerSt: provider.PhaseRunning, adminOK: true},
			want: frpv1alpha1.PhaseReady,
		},
		{
			name: "provider reports Gone -> Lost",
			in:   input{current: frpv1alpha1.PhaseReady, providerSt: provider.PhaseGone, adminOK: false},
			want: frpv1alpha1.PhaseLost,
		},
		{
			name: "provider reports Failed -> Lost (no recovery without manual intervention)",
			in:   input{current: frpv1alpha1.PhaseProvisioning, providerSt: provider.PhaseFailed, adminOK: false},
			want: frpv1alpha1.PhaseLost,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextPhase(tc.in.current, tc.in.providerSt, tc.in.adminOK)
			if got != tc.want {
				t.Errorf("nextPhase(%v, %v, %v) = %v, want %v",
					tc.in.current, tc.in.providerSt, tc.in.adminOK, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run, confirm FAIL**

`devbox run -- go test ./internal/controller/ -run TestNextPhase -v`

- [ ] **Step 3: Implement**

`internal/controller/exitserver_phases.go`:

```go
package controller

import (
	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
)

// nextPhase decides the new ExitPhase from observed provider state and the
// last admin-API probe outcome. Pure function: no side effects, no client
// calls, fully covered by tests in exitserver_phases_test.go.
//
// Intentionally simple. The controller treats Degraded → Unreachable → Lost
// timeouts elsewhere (separate timer logic in Phase 5+). nextPhase
// captures only the local "what should I be right now" decision based on
// the most recent observation.
func nextPhase(current frpv1alpha1.ExitPhase, providerState provider.Phase, adminOK bool) frpv1alpha1.ExitPhase {
	switch providerState {
	case provider.PhaseGone, provider.PhaseFailed:
		return frpv1alpha1.PhaseLost
	case provider.PhaseProvisioning:
		return frpv1alpha1.PhaseProvisioning
	case provider.PhaseRunning:
		if adminOK {
			return frpv1alpha1.PhaseReady
		}
		// Provider says running but admin isn't up yet. If we're already
		// Ready, that's a regression -> Degraded. Otherwise still
		// bootstrapping -> Provisioning.
		if current == frpv1alpha1.PhaseReady {
			return frpv1alpha1.PhaseDegraded
		}
		return frpv1alpha1.PhaseProvisioning
	}
	// No provider observation yet: keep current or default to Pending.
	if current == "" {
		return frpv1alpha1.PhasePending
	}
	return current
}
```

- [ ] **Step 4: Run, confirm 8 sub-tests PASS**

`devbox run -- go test ./internal/controller/ -run TestNextPhase -v`

- [ ] **Step 5: Commit**

```bash
git add internal/controller/exitserver_phases.go internal/controller/exitserver_phases_test.go
git commit -m "feat(controller): pure phase-transition function for ExitServer"
```

---

## Task 3: Reconcile skeleton + finalizer

**Files:**
- Modify: `internal/controller/exitserver_controller.go` (replace the kubebuilder stub)
- Create: `internal/controller/exitserver_finalizer.go`

The full Reconcile pulls a lot of pieces together. This task lands the skeleton that handles deletion and finalizer management; the Provisioner integration and admin probing land in Tasks 4-5.

The controller struct gets:

```go
type ExitServerReconciler struct {
    client.Client
    Scheme       *runtime.Scheme
    Provisioners *provider.Registry  // looked up by spec.provider
    NewAdminClient func(baseURL, user, password string) AdminClient // injection seam for tests
}

type AdminClient interface {
    ServerInfo(ctx context.Context) (*admin.ServerInfo, error)
    PutConfigAndReload(ctx context.Context, body []byte) error
}

const exitServerFinalizer = "frp.operator.io/exitserver-finalizer"
```

`AdminClient` is an interface so tests can inject a fake. The default `NewAdminClient` returns a real `*admin.Client`.

- [ ] **Step 1: Write `internal/controller/exitserver_finalizer.go`**

```go
package controller

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// exitServerFinalizer is the finalizer string the controller adds to every
// ExitServer it manages. Removed only after the underlying VPS is destroyed
// (or confirmed gone) and per-exit Secrets are cleaned up.
const exitServerFinalizer = "frp.operator.io/exitserver-finalizer"

// hasFinalizer reports whether the named finalizer is on the object.
func hasFinalizer(exit *frpv1alpha1.ExitServer, name string) bool {
	for _, f := range exit.Finalizers {
		if f == name {
			return true
		}
	}
	return false
}

// addFinalizer appends the finalizer if it isn't already present and
// patches the object. Returns true if a patch was sent.
func addFinalizer(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, name string) (bool, error) {
	if hasFinalizer(exit, name) {
		return false, nil
	}
	patch := client.MergeFrom(exit.DeepCopy())
	exit.Finalizers = append(exit.Finalizers, name)
	if err := c.Patch(ctx, exit, patch); err != nil {
		return false, fmt.Errorf("add finalizer: %w", err)
	}
	return true, nil
}

// removeFinalizer drops the finalizer from the object and patches. Returns
// true if a patch was sent.
func removeFinalizer(ctx context.Context, c client.Client, exit *frpv1alpha1.ExitServer, name string) (bool, error) {
	if !hasFinalizer(exit, name) {
		return false, nil
	}
	patch := client.MergeFrom(exit.DeepCopy())
	out := exit.Finalizers[:0]
	for _, f := range exit.Finalizers {
		if f != name {
			out = append(out, f)
		}
	}
	exit.Finalizers = out
	if err := c.Patch(ctx, exit, patch); err != nil {
		return false, fmt.Errorf("remove finalizer: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 2: Replace `internal/controller/exitserver_controller.go`**

```go
package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/frp/admin"
	"github.com/mtaku3/frp-operator/internal/provider"
)

// AdminClient is the subset of admin.Client the controller uses. Defining
// it as an interface here lets tests inject a fake without spinning up an
// httptest server.
type AdminClient interface {
	ServerInfo(ctx context.Context) (*admin.ServerInfo, error)
	PutConfigAndReload(ctx context.Context, body []byte) error
}

// AdminClientFactory builds an AdminClient pointing at one frps's webServer.
// The default implementation in main.go wraps admin.NewClient.
type AdminClientFactory func(baseURL, user, password string) AdminClient

// ExitServerReconciler reconciles ExitServer CRs.
type ExitServerReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Provisioners   *provider.Registry
	NewAdminClient AdminClientFactory
}

// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frp.operator.io,resources=exitservers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives one ExitServer toward its desired state. The high-level
// state machine:
//
//  1. Fetch the CR; bail if it's gone (apiserver garbage collected).
//  2. If marked for deletion, run the finalizer (Provisioner.Destroy then
//     drop the finalizer) and exit.
//  3. Add the finalizer if missing.
//  4. Resolve the Provisioner from spec.provider.
//  5. Ensure credentials Secret exists.
//  6. If status.providerID is empty, call Provisioner.Create.
//  7. Otherwise, call Provisioner.Inspect and admin.ServerInfo for health.
//  8. Compute next phase via nextPhase(); patch status.
//  9. Requeue at the configured admin-probe interval.
//
// Tasks 4–5 fill in step 6–8. This task lands the skeleton with steps 1–3.
func (r *ExitServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var exit frpv1alpha1.ExitServer
	if err := r.Get(ctx, req.NamespacedName, &exit); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Deletion path.
	if !exit.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &exit)
	}

	// Add finalizer if missing.
	if added, err := addFinalizer(ctx, r.Client, &exit, exitServerFinalizer); err != nil {
		return ctrl.Result{}, err
	} else if added {
		// Re-fetch on next reconcile so we have the patched object.
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 4–8 are filled in by Tasks 4 and 5. For now, just record that
	// we observed the CR and exit the reconcile.
	logger.V(1).Info("reconciling ExitServer", "name", exit.Name, "phase", exit.Status.Phase)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// reconcileDelete tears the resource down, then drops the finalizer.
func (r *ExitServerReconciler) reconcileDelete(ctx context.Context, exit *frpv1alpha1.ExitServer) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if hasFinalizer(exit, exitServerFinalizer) {
		// Find the Provisioner; if it's not registered, we still drop
		// the finalizer rather than wedging the CR forever.
		if r.Provisioners != nil && exit.Status.ProviderID != "" {
			p, err := r.Provisioners.Lookup(string(exit.Spec.Provider))
			switch {
			case err == nil:
				if destroyErr := p.Destroy(ctx, exit.Status.ProviderID); destroyErr != nil {
					return ctrl.Result{}, fmt.Errorf("provisioner Destroy: %w", destroyErr)
				}
			case errors.Is(err, provider.ErrNotRegistered):
				logger.Info("Provisioner not registered; skipping Destroy", "provider", exit.Spec.Provider)
			default:
				return ctrl.Result{}, fmt.Errorf("Provisioner lookup: %w", err)
			}
		}

		// Best-effort delete of the credentials Secret. (Owner refs would
		// also clean it up, but explicit delete is cheap insurance.)
		var sec corev1.Secret
		sec.Name = credentialsSecretName(exit)
		sec.Namespace = exit.Namespace
		if err := r.Delete(ctx, &sec); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete secret: %w", err)
		}

		if _, err := removeFinalizer(ctx, r.Client, exit, exitServerFinalizer); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager wires the controller. Watches ExitServer CRs and owned
// Secrets so the controller is notified of credential drift.
func (r *ExitServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&frpv1alpha1.ExitServer{}).
		Owns(&corev1.Secret{}).
		Named("exitserver").
		Complete(r)
}
```

- [ ] **Step 3: Update existing `exitserver_controller_test.go`**

The kubebuilder-default test creates a stub ExitServer and asserts the (then no-op) reconcile returns no error. With the finalizer logic now in place, the existing test still works (the reconcile adds the finalizer, returns Requeue=true, no error). However, the test's `Spec` may need fields populated to satisfy v1alpha1 validation if envtest is involved.

Run `devbox run -- go test ./internal/controller/ -v` after writing the new files. If the existing scaffold test fails, fix it — typically by setting the Spec fields the schema requires (Provider, Frps.Version, AllowPorts).

- [ ] **Step 4: Run, confirm package builds and existing tests pass**

`devbox run -- go test ./internal/controller/ -v`

If anything fails because the controller now references `provider.Registry` but `main.go` doesn't populate it, that's expected — for unit tests, the reconciler is constructed directly with the registry passed in. main.go wiring happens in Phase 9+.

- [ ] **Step 5: Commit**

```bash
git add internal/controller/exitserver_controller.go internal/controller/exitserver_finalizer.go internal/controller/exitserver_controller_test.go
git commit -m "feat(controller/exitserver): finalizer handling + reconcile skeleton"
```

---

## Task 4: Provisioner integration in Reconcile

**Files:**
- Modify: `internal/controller/exitserver_controller.go`
- Create: `internal/controller/exitserver_admin.go` (admin URL helper)
- Create: `internal/controller/exitserver_provisioner.go` (Provisioner integration helpers)
- Test: integration test in `internal/controller/exitserver_controller_test.go` (Ginkgo, runs against envtest)

This task fleshes out the reconcile loop's middle steps: ensure Secret, ensure Provisioner state (Create or Inspect), translate provider state into ExitServer.status.

The Reconcile flow is now:

```
1. Fetch CR (done in Task 3)
2. Handle deletion (done in Task 3)
3. Add finalizer (done in Task 3)
4. Look up Provisioner via spec.provider
5. ensureCredentialsSecret -> Credentials
6. If status.providerID == "": call Provisioner.Create with composed Spec, set status.providerID/publicIP/phase
7. Else: call Provisioner.Inspect for current state
8. Compute admin client, call ServerInfo for health (this task: assume OK if provider says Running; Task 5 wires the real probe)
9. Compute nextPhase, patch status
10. Requeue at admin-probe interval (default 30s; configurable from SchedulingPolicy in a later phase)
```

This task focuses on steps 4–7 and a basic step 9. Step 8 (real admin probe) lands in Task 5.

- [ ] **Step 1: Write `internal/controller/exitserver_admin.go`**

```go
package controller

import (
	"fmt"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// adminBaseURL returns the http://<publicIP>:<adminPort> URL for the given
// exit. Empty status.publicIP means "not yet provisioned"; callers must
// not call adminBaseURL until status.publicIP is set.
//
// HTTPS is a v2 item (see spec §9). v1 ships HTTP with token auth.
func adminBaseURL(exit *frpv1alpha1.ExitServer) (string, error) {
	if exit.Status.PublicIP == "" {
		return "", fmt.Errorf("admin base URL: status.publicIP not set")
	}
	port := exit.Spec.Frps.AdminPort
	if port == 0 {
		port = 7500 // schema default; defensive
	}
	return fmt.Sprintf("http://%s:%d", exit.Status.PublicIP, port), nil
}

// adminUser is the username the operator presents to frps's webServer
// admin API. The corresponding password is the random AdminPassword in the
// per-exit Secret.
const adminUser = "admin"
```

- [ ] **Step 2: Write `internal/controller/exitserver_provisioner.go`**

```go
package controller

import (
	"context"
	"fmt"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
)

// reconcileProvisioner is the bridge between the controller's view of an
// ExitServer (CR + Status) and the Provisioner SDK. Returns the latest
// observed provider.State, which the caller uses to update status and
// drive nextPhase.
//
// On first call (status.providerID empty), it issues Provisioner.Create
// and records the new ID. On subsequent calls, it Inspects.
func reconcileProvisioner(
	ctx context.Context,
	p provider.Provisioner,
	exit *frpv1alpha1.ExitServer,
	creds Credentials,
) (provider.State, error) {
	if exit.Status.ProviderID == "" {
		spec := provider.Spec{
			Name:           exit.Namespace + "__" + exit.Name,
			Region:         exit.Spec.Region,
			Size:           exit.Spec.Size,
			BindPort:       portOrDefault(exit.Spec.Frps.BindPort, 7000),
			AdminPort:      portOrDefault(exit.Spec.Frps.AdminPort, 7500),
			FrpsConfigTOML: nil, // populated by caller via composeFrpsConfig in Task 5
		}
		_ = creds // Credentials are baked into FrpsConfigTOML by the caller
		st, err := p.Create(ctx, spec)
		if err != nil {
			return provider.State{}, fmt.Errorf("Provisioner.Create: %w", err)
		}
		return st, nil
	}
	st, err := p.Inspect(ctx, exit.Status.ProviderID)
	if err != nil {
		return st, fmt.Errorf("Provisioner.Inspect: %w", err)
	}
	return st, nil
}

func portOrDefault(p int32, def int32) int {
	if p == 0 {
		return int(def)
	}
	return int(p)
}
```

- [ ] **Step 3: Extend the Reconcile body**

In `exitserver_controller.go`, replace the placeholder `logger.V(1).Info(...)` block with:

```go
	// Step 4: Look up Provisioner.
	if r.Provisioners == nil {
		return ctrl.Result{}, errors.New("ExitServerReconciler.Provisioners is nil — wire it in main.go")
	}
	p, err := r.Provisioners.Lookup(string(exit.Spec.Provider))
	if err != nil {
		// Treat a missing Provisioner as a permanent failure on this CR;
		// surface via condition rather than re-enqueueing forever.
		return r.patchStatusCondition(ctx, &exit, "ProviderNotRegistered", err.Error())
	}

	// Step 5: Ensure credentials Secret.
	creds, err := ensureCredentialsSecret(ctx, r.Client, &exit)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Step 6/7: Provisioner state.
	state, provErr := reconcileProvisioner(ctx, p, &exit, creds)
	if provErr != nil {
		// Network/transient errors get re-enqueued. ErrNotFound is treated
		// as "the resource is gone" -> Lost.
		if errors.Is(provErr, provider.ErrNotFound) {
			state.Phase = provider.PhaseGone
		} else {
			return ctrl.Result{}, provErr
		}
	}

	// Step 8 placeholder: assume admin OK if provider says Running. Task 5
	// replaces this with a real ServerInfo call.
	adminOK := state.Phase == provider.PhaseRunning

	// Step 9: Compute nextPhase and patch status.
	patch := client.MergeFrom(exit.DeepCopy())
	if state.ProviderID != "" {
		exit.Status.ProviderID = state.ProviderID
	}
	if state.PublicIP != "" {
		exit.Status.PublicIP = state.PublicIP
	}
	exit.Status.Phase = nextPhase(exit.Status.Phase, state.Phase, adminOK)
	now := metav1.Now()
	exit.Status.LastReconcileTime = &now
	if err := r.Status().Patch(ctx, &exit, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch status: %w", err)
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
```

Add a helper:

```go
// patchStatusCondition writes one Condition to ExitServer.status and
// returns a non-requeueing Result (the issue is "permanent" per this call;
// resolution requires a spec change).
func (r *ExitServerReconciler) patchStatusCondition(
	ctx context.Context,
	exit *frpv1alpha1.ExitServer,
	reason, message string,
) (ctrl.Result, error) {
	patch := client.MergeFrom(exit.DeepCopy())
	cond := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: exit.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	exit.Status.Conditions = upsertCondition(exit.Status.Conditions, cond)
	if err := r.Status().Patch(ctx, exit, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// upsertCondition replaces an existing condition by Type or appends one.
func upsertCondition(in []metav1.Condition, c metav1.Condition) []metav1.Condition {
	for i, existing := range in {
		if existing.Type == c.Type {
			in[i] = c
			return in
		}
	}
	return append(in, c)
}
```

Adjust imports: `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`, `"github.com/mtaku3/frp-operator/internal/provider"`, etc. The existing imports may need cleanup.

- [ ] **Step 4: Add envtest integration test**

Append to `internal/controller/exitserver_controller_test.go` (Ginkgo, runs in envtest):

```go
package controller

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
	"github.com/mtaku3/frp-operator/internal/provider/fake"
)

var _ = Describe("ExitServerController integration", func() {
	ctx := context.Background()

	var (
		fakeProv *fake.FakeProvisioner
		registry *provider.Registry
		recon    *ExitServerReconciler
	)

	BeforeEach(func() {
		fakeProv = fake.New("digitalocean") // match enum value used in spec
		registry = provider.NewRegistry()
		Expect(registry.Register(fakeProv)).To(Succeed())
		recon = &ExitServerReconciler{
			Client:       k8sClient,
			Scheme:       scheme.Scheme,
			Provisioners: registry,
		}
	})

	It("provisions a fresh ExitServer and writes status", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "exit-int", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				Region:         "nyc1",
				Size:           "s-1vcpu-1gb",
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())
		t.Cleanup(func() {
			_ = k8sClient.Delete(ctx, exit)
		})

		// First reconcile: adds finalizer.
		_, err := recon.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "exit-int", Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())

		// Second reconcile: creates Secret + Provisioner.Create.
		_, err = recon.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "exit-int", Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())

		// Status now reflects the provisioned state.
		got := &frpv1alpha1.ExitServer{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "exit-int", Namespace: "default"}, got)).To(Succeed())
		Expect(got.Status.ProviderID).NotTo(BeEmpty())
		Expect(got.Status.PublicIP).To(Equal("127.0.0.1"))
		Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseReady))

		// Secret was created.
		var sec corev1.Secret
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "exit-int-credentials", Namespace: "default"}, &sec)).To(Succeed())
		Expect(sec.Data["admin-password"]).NotTo(BeEmpty())
		Expect(sec.Data["auth-token"]).NotTo(BeEmpty())
	})

	It("destroys the underlying resource on CR delete", func() {
		exit := &frpv1alpha1.ExitServer{
			ObjectMeta: metav1.ObjectMeta{Name: "exit-del", Namespace: "default"},
			Spec: frpv1alpha1.ExitServerSpec{
				Provider:       frpv1alpha1.ProviderDigitalOcean,
				Region:         "nyc1",
				Size:           "s-1vcpu-1gb",
				CredentialsRef: frpv1alpha1.SecretKeyRef{Name: "x", Key: "y"},
				Frps:           frpv1alpha1.FrpsConfig{Version: "v0.68.1"},
				AllowPorts:     []string{"1024-65535"},
			},
		}
		Expect(k8sClient.Create(ctx, exit)).To(Succeed())

		// Drive provisioning so we have a ProviderID.
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "exit-del", Namespace: "default"}}
		for i := 0; i < 3; i++ {
			_, err := recon.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}
		got := &frpv1alpha1.ExitServer{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "exit-del", Namespace: "default"}, got)).To(Succeed())
		providerID := got.Status.ProviderID
		Expect(providerID).NotTo(BeEmpty())

		// Delete the CR; reconcile should destroy and remove finalizer.
		Expect(k8sClient.Delete(ctx, got)).To(Succeed())
		Eventually(func() error {
			_, err := recon.Reconcile(ctx, req)
			return err
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// CR is gone, Fake provisioner no longer holds the resource.
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "exit-del", Namespace: "default"}, got)
		Expect(err).To(HaveOccurred()) // 404
		_, inspectErr := fakeProv.Inspect(ctx, providerID)
		Expect(errors.Is(inspectErr, provider.ErrNotFound)).To(BeTrue())
	})
})
```

The `t.Cleanup` line above won't compile in a Ginkgo Describe (no `*testing.T` available). Replace with `DeferCleanup(...)`:

```go
DeferCleanup(func() {
    _ = k8sClient.Delete(ctx, exit)
})
```

The first integration test is sufficient for Phase 5 Task 4. The deletion test exercises Task 3's finalizer code too — keep it.

- [ ] **Step 5: Run, confirm PASS**

`devbox run -- make test`

Both controller specs (the existing CRD-install ones from Phase 1 + the new ExitServer integration) should pass. Total Ginkgo specs ≥ 6.

If the integration test fails because `Reconcile` doesn't terminate properly (e.g., the second reconcile sees the Secret already created and skips Provisioner.Create), inspect the logs: the controller should idempotently call Provisioner.Inspect once `status.providerID` is set.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/exitserver_provisioner.go internal/controller/exitserver_admin.go internal/controller/exitserver_controller.go internal/controller/exitserver_controller_test.go
git commit -m "feat(controller/exitserver): provision via Provisioner registry, write status on reconcile"
```

---

## Task 5: Real admin-API health probe

**Files:**
- Modify: `internal/controller/exitserver_controller.go`
- Test: extend `internal/controller/exitserver_controller_test.go`

Replace the `adminOK := state.Phase == provider.PhaseRunning` placeholder with a real call to `AdminClient.ServerInfo`. This requires:

1. Building an `AdminClient` once `state.PublicIP` is known.
2. Calling `ServerInfo(ctx)` with a short timeout; the returned `nil` error means admin OK.
3. Threading the `NewAdminClient` factory injection seam through reconcile.
4. Pushing the rendered frps.toml via `PutConfigAndReload` when the controller observes config drift (`Spec.Frps.*` changed since last reconcile, or `status.frpsVersion != Spec.Frps.Version`).

For Phase 5, the config-push side is minimal: push exactly once (after Provisioner.Create completes and admin is reachable), then never again. Spec-change-driven re-push lands in Phase 5+ refinements or in Phase 6 alongside Tunnel proxy push.

- [ ] **Step 1: Add `AdminClientFactory` plumbing**

In `exitserver_controller.go`, ensure the struct already has `NewAdminClient AdminClientFactory` (added in Task 3). In Reconcile, after `state.PublicIP` is set:

```go
	// Step 8: real admin probe.
	adminOK := false
	if state.PublicIP != "" && r.NewAdminClient != nil {
		baseURL, err := adminBaseURL(&exit)
		if err == nil {
			ac := r.NewAdminClient(baseURL, adminUser, creds.AdminPassword)
			probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if _, sErr := ac.ServerInfo(probeCtx); sErr == nil {
				adminOK = true
			}
		}
	}
```

- [ ] **Step 2: Inject a fake AdminClient in tests**

The integration test from Task 4 didn't set `NewAdminClient`, so the controller's adminOK is always false → status stays Provisioning even after Fake says Running. Update the test setup:

```go
type fakeAdmin struct {
    serverInfoOK bool
}

func (f *fakeAdmin) ServerInfo(ctx context.Context) (*admin.ServerInfo, error) {
    if !f.serverInfoOK {
        return nil, errors.New("not yet ready")
    }
    return &admin.ServerInfo{Version: "0.68.1"}, nil
}

func (f *fakeAdmin) PutConfigAndReload(ctx context.Context, body []byte) error {
    return nil
}

// In BeforeEach:
fa := &fakeAdmin{serverInfoOK: true}
recon.NewAdminClient = func(_, _, _ string) AdminClient { return fa }
```

- [ ] **Step 3: Re-run integration tests**

`devbox run -- make test`

The "provisions a fresh ExitServer" test should now reliably reach `PhaseReady` because the fake admin returns OK.

Add one more test case: admin probe fails ⇒ Degraded.

```go
It("transitions to Degraded when admin probe fails post-Ready", func() {
    fa := &fakeAdmin{serverInfoOK: true}
    recon.NewAdminClient = func(_, _, _ string) AdminClient { return fa }

    exit := &frpv1alpha1.ExitServer{...}
    Expect(k8sClient.Create(ctx, exit)).To(Succeed())
    DeferCleanup(func() { _ = k8sClient.Delete(ctx, exit) })

    req := ctrl.Request{NamespacedName: types.NamespacedName{Name: exit.Name, Namespace: "default"}}
    for i := 0; i < 3; i++ {
        _, err := recon.Reconcile(ctx, req)
        Expect(err).NotTo(HaveOccurred())
    }
    got := &frpv1alpha1.ExitServer{}
    Expect(k8sClient.Get(ctx, types.NamespacedName{Name: exit.Name, Namespace: "default"}, got)).To(Succeed())
    Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseReady))

    // Now flip the fake admin to fail.
    fa.serverInfoOK = false
    _, err := recon.Reconcile(ctx, req)
    Expect(err).NotTo(HaveOccurred())
    Expect(k8sClient.Get(ctx, types.NamespacedName{Name: exit.Name, Namespace: "default"}, got)).To(Succeed())
    Expect(got.Status.Phase).To(Equal(frpv1alpha1.PhaseDegraded))
})
```

- [ ] **Step 4: Run, confirm all pass**

`devbox run -- make test`

- [ ] **Step 5: Commit**

```bash
git add internal/controller/exitserver_controller.go internal/controller/exitserver_controller_test.go
git commit -m "feat(controller/exitserver): real admin-API health probe with injected client"
```

---

## Phase 5 done — exit criteria

- `devbox run -- make test` green: api/v1alpha1 + bootstrap + frp/* + provider + provider/fake + provider/localdocker + scheduler + controller (with new ExitServer integration tests).
- `internal/controller/exitserver_controller.go` reconciles ExitServer through Pending → Provisioning → Ready → Degraded → Lost transitions.
- Finalizer correctly handles deletion: Provisioner.Destroy + Secret cleanup + finalizer removal.
- Per-exit Credentials Secret managed (created on first reconcile, owner-ref to the ExitServer).
- Admin API probe injected via `AdminClientFactory` (allows tests without a real `frps`).
- TunnelController, ServiceWatcher, real DigitalOcean, ExitReclaim all still stubs/missing — those are Phase 6/7/8/9.

The next plan (Phase 6: TunnelController) handles Tunnel CR reconcile: scheduler-driven exit selection, port allocation in ExitServer.status, frpc Deployment + Secret creation, frps config push to expose proxies.
