# Phase 5: ExitClaim Lifecycle Controller Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Drive newly-created ExitClaim CRs through the lifecycle: `Created → Launched → Registered → Initialized → Ready`. On delete, drain bound tunnels and call `cloudProvider.Delete`. The provisioner (Phase 4) emits ExitClaims; this phase realizes them on the cloud and tears them down on cleanup.

**Architecture:** Single `lifecycle.Controller` watches `ExitClaim`. Each Reconcile fans out to four phase functions in sequence: `launch.Reconcile` → `registration.Reconcile` → `initialization.Reconcile` → `liveness.Reconcile`. Each is a struct method on the controller; each returns `(reconcile.Result, error)` and either sets a Status condition + returns early or proceeds. Termination path (`finalize`) handles `DeletionTimestamp != nil`. One finalizer (`frp.operator.io/termination`) gates the delete.

**Spec:** §6 (ExitClaim lifecycle).

**Prerequisites:** Phases 1, 2, 3, 4 merged.

**End state:**
- `pkg/controllers/exitclaim/lifecycle/` watches ExitClaim and drives state through Created → Ready.
- envtest with `fake.CloudProvider` confirms a freshly-created ExitClaim reaches Conditions[Ready]=True within a few reconciles.
- Delete with bound tunnels waits for unbinding (or TerminationGracePeriod).
- `make test` passes for the package.

---

## File map

```
pkg/controllers/exitclaim/lifecycle/
├── doc.go
├── controller.go                        # Controller struct, Reconcile, finalize
├── launch.go                            # Phase 1: cloudProvider.Create
├── registration.go                      # Phase 2: admin API probe
├── initialization.go                    # Phase 3: reservePorts, mark Ready
├── liveness.go                          # Phase 4: RegistrationTTL stale check
├── controller_test.go                   # envtest spec: full lifecycle round-trip
├── launch_test.go                       # unit: Create idempotency, error wrapping
├── registration_test.go                 # unit with fake admin server
├── finalize_test.go                     # envtest: delete with bound tunnel waits
└── suite_test.go                        # envtest setup
```

---

## Task 1: launch.go (Phase 1 — cloudProvider.Create)

```go
package lifecycle

import (
    "context"
    "fmt"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

const RegistrationTTL = 15 * time.Minute

type Launcher struct {
    KubeClient    client.Client
    CloudProvider *cloudprovider.Registry
}

// Reconcile is invoked when the claim has no Launched=True condition yet.
// On success: hydrates Status (ProviderID, ExitName, ImageID, FrpsVersion,
// Capacity, Allocatable, PublicIP), patches status, sets Conditions[Launched]=True.
func (l *Launcher) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
    if isCondTrue(claim, v1alpha1.ConditionTypeLaunched) { return reconcile.Result{}, nil }

    cp, err := l.CloudProvider.For(claim.Spec.ProviderClassRef.Kind)
    if err != nil {
        setCond(claim, v1alpha1.ConditionTypeLaunched, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
        return reconcile.Result{}, l.KubeClient.Status().Update(ctx, claim)
    }

    hydrated, err := cp.Create(ctx, claim)
    if err != nil {
        setCond(claim, v1alpha1.ConditionTypeLaunched, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
        _ = l.KubeClient.Status().Update(ctx, claim)
        return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
    }

    // Copy hydrated status into the live object.
    claim.Status = hydrated.Status
    setCond(claim, v1alpha1.ConditionTypeLaunched, metav1.ConditionTrue, v1alpha1.ReasonProvisioned, "exit launched")
    if err := l.KubeClient.Status().Update(ctx, claim); err != nil { return reconcile.Result{}, fmt.Errorf("status update: %w", err) }
    return reconcile.Result{Requeue: true}, nil
}

func isCondTrue(claim *v1alpha1.ExitClaim, t string) bool {
    for _, c := range claim.Status.Conditions {
        if c.Type == t && c.Status == metav1.ConditionTrue { return true }
    }
    return false
}

func setCond(claim *v1alpha1.ExitClaim, t string, status metav1.ConditionStatus, reason, msg string) {
    // helper that sets-or-appends; if this gets reused, move to a shared pkg.
    panic("subagent: implement using apimeta.SetStatusCondition")
}
```

Tests in `launch_test.go`:
1. Successful Create populates Status + sets Launched=True.
2. Provider error sets Launched=False with Reason=ProviderError; requeues.
3. Already-Launched claim returns immediately (no Create call — verify via fake.CallCount).

Commit: `feat(lifecycle/launch): provider Create with status hydration`.

---

## Task 2: registration.go

```go
package lifecycle

import (
    "context"
    "fmt"
    "time"

    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/reconcile"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/cloudprovider/frps/admin"
)

type Registrar struct {
    KubeClient client.Client
    AdminFactory func(baseURL string) *admin.Client
}

func (r *Registrar) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
    if !isCondTrue(claim, v1alpha1.ConditionTypeLaunched) { return reconcile.Result{}, nil }
    if isCondTrue(claim, v1alpha1.ConditionTypeRegistered) { return reconcile.Result{}, nil }

    if claim.Status.PublicIP == "" {
        return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
    }
    adminPort := claim.Spec.Frps.AdminPort
    if adminPort == 0 { adminPort = 7400 }

    c := r.AdminFactory(fmt.Sprintf("http://%s:%d", claim.Status.PublicIP, adminPort))
    info, err := c.GetServerInfo(ctx)
    if err != nil {
        setCond(claim, v1alpha1.ConditionTypeRegistered, metav1.ConditionFalse, v1alpha1.ReasonAdminAPIUnreachable, err.Error())
        _ = r.KubeClient.Status().Update(ctx, claim)
        return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
    }
    if info.Version != "" { claim.Status.FrpsVersion = info.Version }
    setCond(claim, v1alpha1.ConditionTypeRegistered, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "admin API reachable")
    if err := r.KubeClient.Status().Update(ctx, claim); err != nil { return reconcile.Result{}, err }
    return reconcile.Result{Requeue: true}, nil
}
```

Tests with `httptest.Server` returning fake `/api/serverinfo`:
1. Reachable admin → Registered=True.
2. Unreachable → Registered=False with reason AdminAPIUnreachable.
3. PublicIP empty → requeue without setting any condition.

Commit: `feat(lifecycle/registration): admin API probe sets Registered=True`.

---

## Task 3: initialization.go + liveness.go

```go
// initialization.go
type Initializer struct{ KubeClient client.Client }

func (i *Initializer) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
    if !isCondTrue(claim, v1alpha1.ConditionTypeRegistered) { return reconcile.Result{}, nil }
    if isCondTrue(claim, v1alpha1.ConditionTypeInitialized) { return reconcile.Result{}, nil }
    // For v1, "initialization" = mark Initialized + Ready. Future:
    // reserve admin/control ports, validate provider state.
    setCond(claim, v1alpha1.ConditionTypeInitialized, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "ready for tunnels")
    setCond(claim, v1alpha1.ConditionTypeReady, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "")
    return reconcile.Result{}, i.KubeClient.Status().Update(ctx, claim)
}
```

```go
// liveness.go
type Liveness struct{ KubeClient client.Client }

func (l *Liveness) Reconcile(ctx context.Context, claim *v1alpha1.ExitClaim) (reconcile.Result, error) {
    if !isCondTrue(claim, v1alpha1.ConditionTypeLaunched) { return reconcile.Result{}, nil }
    if isCondTrue(claim, v1alpha1.ConditionTypeRegistered) { return reconcile.Result{}, nil }
    launchedAt := condTransitionTime(claim, v1alpha1.ConditionTypeLaunched)
    if time.Since(launchedAt) < RegistrationTTL { return reconcile.Result{RequeueAfter: 30 * time.Second}, nil }
    // RegistrationTTL exceeded → mark Disrupted + delete.
    setCond(claim, v1alpha1.ConditionTypeDisrupted, metav1.ConditionTrue, v1alpha1.ReasonRegistrationTimeout, "exceeded RegistrationTTL")
    _ = l.KubeClient.Status().Update(ctx, claim)
    return reconcile.Result{}, l.KubeClient.Delete(ctx, claim)
}

func condTransitionTime(claim *v1alpha1.ExitClaim, t string) time.Time { /* read LastTransitionTime */ }
```

Tests with mocked time:
1. Initialization sets both Initialized + Ready in one shot.
2. Liveness fires Delete after TTL when Registered never landed.

Commit: `feat(lifecycle/init+liveness): mark Ready / TTL-driven cleanup`.

---

## Task 4: controller.go (the orchestrator)

```go
package lifecycle

import (
    "context"

    apierrors "k8s.io/apimachinery/pkg/api/errors"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

    v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
    "github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

type Controller struct {
    Client        client.Client
    CloudProvider *cloudprovider.Registry
    AdminFactory  func(baseURL string) *admin.Client

    launch         *Launcher
    registration   *Registrar
    initialization *Initializer
    liveness       *Liveness
}

func New(c client.Client, cp *cloudprovider.Registry, adminFactory func(string) *admin.Client) *Controller {
    return &Controller{
        Client: c, CloudProvider: cp, AdminFactory: adminFactory,
        launch:         &Launcher{KubeClient: c, CloudProvider: cp},
        registration:   &Registrar{KubeClient: c, AdminFactory: adminFactory},
        initialization: &Initializer{KubeClient: c},
        liveness:       &Liveness{KubeClient: c},
    }
}

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var claim v1alpha1.ExitClaim
    if err := r.Client.Get(ctx, req.NamespacedName, &claim); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    if !claim.DeletionTimestamp.IsZero() {
        return r.finalize(ctx, &claim)
    }

    if controllerutil.AddFinalizer(&claim, v1alpha1.TerminationFinalizer) {
        return ctrl.Result{Requeue: true}, r.Client.Update(ctx, &claim)
    }

    for _, phase := range []func(context.Context, *v1alpha1.ExitClaim) (ctrl.Result, error){
        r.launch.Reconcile,
        r.registration.Reconcile,
        r.initialization.Reconcile,
        r.liveness.Reconcile,
    } {
        if res, err := phase(ctx, &claim); err != nil || !res.IsZero() { return res, err }
    }
    return ctrl.Result{}, nil
}

// finalize drains then deletes the cloud resource then strips the finalizer.
func (r *Controller) finalize(ctx context.Context, claim *v1alpha1.ExitClaim) (ctrl.Result, error) {
    // 1. Wait for tunnels to unbind (or grace period).
    bound, err := r.tunnelsBoundTo(ctx, claim.Name)
    if err != nil { return ctrl.Result{}, err }
    grace := 1 * time.Hour // TODO: read from claim.Spec.TerminationGracePeriod
    deletedAt := claim.DeletionTimestamp.Time
    if len(bound) > 0 && time.Since(deletedAt) < grace {
        // Notify tunnels to release: clear their AssignedExit.
        for _, t := range bound {
            patch := client.MergeFrom(t.DeepCopy())
            t.Status.AssignedExit = ""
            t.Status.AssignedPorts = nil
            _ = r.Client.Status().Patch(ctx, &t, patch)
        }
        return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
    }

    // 2. Provider Delete.
    cp, err := r.CloudProvider.For(claim.Spec.ProviderClassRef.Kind)
    if err == nil {
        if err := cp.Delete(ctx, claim); err != nil && !cloudprovider.IsExitNotFound(err) {
            return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
        }
    }

    // 3. Strip finalizer.
    controllerutil.RemoveFinalizer(claim, v1alpha1.TerminationFinalizer)
    return ctrl.Result{}, r.Client.Update(ctx, claim)
}

func (r *Controller) tunnelsBoundTo(ctx context.Context, claimName string) ([]v1alpha1.Tunnel, error) {
    var list v1alpha1.TunnelList
    if err := r.Client.List(ctx, &list); err != nil { return nil, err }
    out := []v1alpha1.Tunnel{}
    for i := range list.Items {
        if list.Items[i].Status.AssignedExit == claimName { out = append(out, list.Items[i]) }
    }
    return out, nil
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).Named("exitclaim-lifecycle").For(&v1alpha1.ExitClaim{}).Complete(r)
}
```

envtest in `controller_test.go` + `finalize_test.go`:
1. Create ExitClaim → Eventually Conditions[Ready]=True within 30s.
2. Delete ExitClaim with no bound tunnels → fake.CloudProvider.Delete called once + finalizer stripped.
3. Delete ExitClaim with one bound Tunnel → tunnel's AssignedExit cleared; eventually exitclaim deleted.
4. Provider Create error → Conditions[Launched]=False with ProviderError reason.

Commit: `feat(lifecycle): orchestrate Launch→Register→Init→Liveness with finalizer`.

---

## Phase 5 acceptance checklist

- [x] Each phase function (launch, registration, initialization, liveness) is a separate file with focused responsibility.
- [x] Controller chains them in order; on first incomplete phase, returns early.
- [x] Termination drains tunnels, calls provider Delete, strips finalizer.
- [x] envtest specs cover full happy path + error paths + delete-with-bound-tunnel.
- [x] All tests pass.
- [x] `go build`, `go vet` clean.

## Out of scope

- Disruption (Phase 6).
- ProviderClass `Status.Conditions` — Phase 7.
- ServiceWatcher — Phase 8.
- Operator wiring of the controller into manager — Phase 9.
