# Phase 9: Operator Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans.

**Goal:** Restore the manager binary. Build `pkg/operator/`, wire all controllers from Phases 3-8 into one `manager.Manager` with leader election + indexers + healthz/readyz + metrics. **No webhooks** (cert-manager / pkg/certs / webhook server stay deleted; CEL on CRDs is the only validation surface).

**Architecture:** `pkg/operator/operator.go` constructs the manager + cluster + cloudprovider Registry. Each provider's `New(kube, ...)` is called and registered. Then each controller package's `SetupWithManager` is invoked. Field indexers added per spec §10. Health/ready probes per spec §10. Metrics per spec §13. `cmd/manager/main.go` wires CLI flags + env vars + signal handling and calls `operator.Run(ctx)`.

**Spec:** §10 (operator wiring), §11 (feature gates), §12 (settings), §13 (metrics).

**Prerequisites:** Phases 1-8 merged. Phase 5 + 6 + 7 + 8 each provided `SetupWithManager`-style hooks.

**End state:**
- `pkg/operator/operator.go` provides `Run(ctx)`.
- `cmd/manager/main.go` parses flags + env, calls `operator.Run`.
- Manager starts; controllers reconcile; healthz on `:8081/healthz`; readyz on `:8081/readyz`; metrics on `:8080/metrics`.
- Leader election uses Lease in `frp-operator-system` namespace.
- envtest integration test: launch the operator under envtest, create a Tunnel + ExitPool + LocalDockerProviderClass, observe ExitClaim creation (via fake provider). Tests prove the wiring is correct end-to-end.

---

## File map

```
pkg/operator/
├── doc.go
├── operator.go                          # Run(ctx) — top-level wiring
├── config.go                            # Config struct (env + flags)
├── flags.go                             # cli.Parse → Config
├── indexers.go                          # field indexers
├── health.go                            # healthz/readyz checks
├── feature_gates.go                     # --feature-gates parser
├── operator_test.go                     # envtest: full end-to-end smoke
└── suite_test.go

cmd/manager/main.go                       # CLI entrypoint, calls operator.Run
```

---

## Task 1: config.go + flags.go + feature_gates.go

`Config` mirrors spec §12 settings:

```go
type Config struct {
    KubeConfig         string
    LeaderElection     bool
    LeaderElectionID   string
    LeaderElectionNS   string
    DisableProfiling   bool

    BatchIdleDuration  time.Duration
    BatchMaxDuration   time.Duration
    KubeClientQPS      float32
    KubeClientBurst    int

    LogLevel           string
    MetricsAddr        string
    HealthProbeAddr    string

    PreferencePolicy   string // Respect | Ignore
    MinValuesPolicy    string // Strict | BestEffort

    RegistrationTTL    time.Duration
    DriftTTL           time.Duration
    DisruptionPollPeriod time.Duration

    FeatureGates       map[string]bool
}
```

`flags.go` parses CLI flags + env vars (CLI > env > default). Use `flag` stdlib or `pflag`. Each spec key maps to one flag; document defaults inline.

`feature_gates.go` parses `--feature-gates=Name=true,Name2=false`. Known gates from spec §11: `StaticReplicas`, `ExitRepair`, `InterruptionHandling`, `ConsolidationDryRun`, `MultiPoolBinpacking`. Unknown gates → error at parse.

Tests: per-flag parse, env override, feature-gate parser handles all spec-listed gates.

Commit: `feat(operator): config, flags, feature gates`.

---

## Task 2: indexers.go

Per spec §10:

```go
func setupIndexers(ctx context.Context, mgr ctrl.Manager) error {
    if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.ExitClaim{}, "status.providerID", func(o client.Object) []string {
        c := o.(*v1alpha1.ExitClaim); if c.Status.ProviderID == "" { return nil }; return []string{c.Status.ProviderID}
    }); err != nil { return err }
    if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.ExitClaim{}, "spec.providerClassRef.name", func(o client.Object) []string {
        return []string{o.(*v1alpha1.ExitClaim).Spec.ProviderClassRef.Name}
    }); err != nil { return err }
    if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Tunnel{}, "status.assignedExit", func(o client.Object) []string {
        t := o.(*v1alpha1.Tunnel); if t.Status.AssignedExit == "" { return nil }; return []string{t.Status.AssignedExit}
    }); err != nil { return err }
    return nil
}
```

Tests: with envtest manager, create objects, query via `client.MatchingFields{"status.providerID": id}` → finds expected.

Commit: `feat(operator): field indexers for ExitClaim providerID + ProviderClassRef + Tunnel.AssignedExit`.

---

## Task 3: health.go

```go
func setupHealthChecks(mgr ctrl.Manager) error {
    if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil { return err }
    if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil { return err }
    if err := mgr.AddReadyzCheck("crd", crdReadinessCheck(mgr.GetClient())); err != nil { return err }
    return nil
}

func crdReadinessCheck(c client.Client) healthz.Checker {
    return func(req *http.Request) error {
        ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second); defer cancel()
        if err := c.List(ctx, &v1alpha1.ExitPoolList{}, client.Limit(1)); err != nil {
            return fmt.Errorf("ExitPool CRD not ready: %w", err)
        }
        if err := c.List(ctx, &v1alpha1.ExitClaimList{}, client.Limit(1)); err != nil {
            return fmt.Errorf("ExitClaim CRD not ready: %w", err)
        }
        if err := c.List(ctx, &v1alpha1.TunnelList{}, client.Limit(1)); err != nil {
            return fmt.Errorf("Tunnel CRD not ready: %w", err)
        }
        return nil
    }
}
```

Tests: `httptest` against the readyz endpoint after envtest manager is up.

Commit: `feat(operator): healthz/readyz with CRD presence check`.

---

## Task 4: operator.go (the wiring)

```go
func Run(ctx context.Context, cfg *Config) error {
    log := log.FromContext(ctx)
    scheme := runtime.NewScheme()
    if err := corev1.AddToScheme(scheme); err != nil { return err }
    if err := v1alpha1.AddToScheme(scheme); err != nil { return err }
    if err := ldv1alpha1.AddToScheme(scheme); err != nil { return err }
    if err := dov1alpha1.AddToScheme(scheme); err != nil { return err }

    restCfg, err := ctrl.GetConfig(); if err != nil { return err }
    restCfg.QPS = cfg.KubeClientQPS
    restCfg.Burst = cfg.KubeClientBurst

    mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
        Scheme: scheme,
        LeaderElection:                cfg.LeaderElection,
        LeaderElectionID:              cfg.LeaderElectionID,
        LeaderElectionNamespace:       cfg.LeaderElectionNS,
        LeaderElectionReleaseOnCancel: true,
        LeaderElectionResourceLock:    "leases",
        Metrics:                       server.Options{BindAddress: cfg.MetricsAddr},
        HealthProbeBindAddress:        cfg.HealthProbeAddr,
    })
    if err != nil { return err }

    if err := setupIndexers(ctx, mgr); err != nil { return err }
    if err := setupHealthChecks(mgr); err != nil { return err }

    cluster := state.NewCluster(mgr.GetClient())
    cpRegistry := cloudprovider.NewRegistry()
    if cp, err := localdocker.New(mgr.GetClient()); err == nil {
        if err := cpRegistry.Register("LocalDockerProviderClass", cp); err != nil { return err }
    } else {
        log.Info("localdocker provider unavailable", "err", err)
    }
    if cp, err := digitalocean.New(mgr.GetClient(), ""); err == nil {
        if err := cpRegistry.Register("DigitalOceanProviderClass", cp); err != nil { return err }
    }

    // Informer controllers (Phase 3) — write into cluster.
    if err := (&informer.ExitClaimController{Client: mgr.GetClient(), Cluster: cluster}).SetupWithManager(mgr); err != nil { return err }
    if err := (&informer.ExitPoolController{Client: mgr.GetClient(), Cluster: cluster}).SetupWithManager(mgr); err != nil { return err }
    if err := (&informer.TunnelController{Client: mgr.GetClient(), Cluster: cluster}).SetupWithManager(mgr); err != nil { return err }
    // Per-provider ProviderClass watchers
    if err := (&informer.ProviderClassController{Client: mgr.GetClient(), Cluster: cluster, Watch: &ldv1alpha1.LocalDockerProviderClass{}}).SetupWithManager(mgr); err != nil { return err }
    if err := (&informer.ProviderClassController{Client: mgr.GetClient(), Cluster: cluster, Watch: &dov1alpha1.DigitalOceanProviderClass{}}).SetupWithManager(mgr); err != nil { return err }

    // Provisioner (Phase 4) — singleton.
    prov := provisioning.New(cluster, mgr.GetClient(), cpRegistry)
    if err := prov.SetupWithManager(mgr); err != nil { return err }
    cluster.SetTriggers(func() { prov.Trigger("__cluster__") }, nil) // disruption trigger wired below.
    if err := (&provisioning.PodController{Client: mgr.GetClient(), Batcher: prov.Batcher}).SetupWithManager(mgr); err != nil { return err }
    if err := (&provisioning.NodeController{Client: mgr.GetClient(), Batcher: prov.Batcher}).SetupWithManager(mgr); err != nil { return err }

    // Lifecycle (Phase 5)
    lifecycleCtrl := lifecycle.New(mgr.GetClient(), cpRegistry, func(baseURL string) *admin.Client { return admin.New(baseURL) })
    if err := lifecycleCtrl.SetupWithManager(mgr); err != nil { return err }

    // Disruption (Phase 6) — singleton.
    disrupt := disruption.New(cluster, mgr.GetClient(), &disruption.Queue{Client: mgr.GetClient(), Cluster: cluster, Provisioner: prov}, disruptionMethods(cluster))
    if err := disrupt.SetupWithManager(mgr); err != nil { return err }
    cluster.SetTriggers(func() { prov.Trigger("__cluster__") }, func() { /* disruption is on a 10s ticker, no immediate trigger needed */ })

    // ExitPool ancillary (Phase 7)
    if err := (&hash.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil { return err }
    if err := (&counter.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil { return err }
    if err := (&readiness.Controller{Client: mgr.GetClient(), KindToObject: providerClassFactories(cpRegistry)}).SetupWithManager(mgr); err != nil { return err }
    if err := (&validation.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil { return err }

    // ServiceWatcher (Phase 8)
    if err := (&servicewatcher.Controller{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil { return err }
    if err := (&servicewatcher.ReverseSync{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil { return err }

    log.Info("operator starting")
    return mgr.Start(ctx)
}

func disruptionMethods(c *state.Cluster) []disruption.Method {
    return []disruption.Method{
        &methods.Emptiness{Cluster: c, Now: time.Now},
        &methods.StaticDrift{},
        &methods.Drift{},
        &methods.Expiration{},
        &methods.MultiNodeConsolidation{},
        &methods.SingleNodeConsolidation{},
    }
}

func providerClassFactories(reg *cloudprovider.Registry) map[string]func() client.Object {
    out := map[string]func() client.Object{}
    for _, kind := range reg.Kinds() {
        cp, _ := reg.For(kind)
        for _, obj := range cp.GetSupportedProviderClasses() {
            kind := obj.GetObjectKind().GroupVersionKind().Kind
            objCopy := obj.DeepCopyObject().(client.Object)
            out[kind] = func() client.Object { return objCopy.DeepCopyObject().(client.Object) }
        }
    }
    return out
}
```

Note: this is a LARGE wiring function. Subagent should split per-Phase blocks into helper functions if it grows past ~150 lines.

Commit: `feat(operator): top-level Run(ctx) wires all controllers + provider registry`.

---

## Task 5: cmd/manager/main.go

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/mtaku3/frp-operator/pkg/operator"

    "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
    cfg, err := operator.LoadConfig()
    if err != nil { log.Fatal(err) }

    zapLog := zap.New(zap.UseDevMode(false))
    ctx := zap.IntoContext(signalContext(), zapLog)

    if err := operator.Run(ctx, cfg); err != nil {
        log.Fatalf("operator: %v", err)
        os.Exit(1)
    }
}

func signalContext() context.Context {
    return ctrl.SetupSignalHandler()
}
```

Commit: `feat(cmd/manager): wire main.go to operator.Run`.

---

## Task 6: operator_test.go

Spin up envtest + the operator and verify basic flow:

1. Apply LocalDockerProviderClass (skip if Docker unreachable; or use a fake-class equivalent).
2. Apply ExitPool referencing the class.
3. Apply Tunnel.
4. Eventually: ExitClaim exists in API + Tunnel.Status.AssignedExit set.

Commit: `test(operator): end-to-end envtest smoke`.

---

## Phase 9 acceptance checklist

- [x] `pkg/operator/operator.go` wires all controllers + provider registry under one manager.
- [x] No webhooks. No `pkg/certs/`. CEL on CRDs is sole validation.
- [x] Field indexers per spec §10.
- [x] healthz/readyz with CRD presence check.
- [x] Metrics + leader election + ReleaseOnCancel.
- [x] cmd/manager/main.go is a thin wrapper.
- [x] envtest smoke test passes.

## Out of scope

- Conversion webhooks (no v1beta1 yet).
- Auto-repair / interruption handler — feature-gated, not implemented.
- Static-replicas mode — gated, not implemented.
- E2E in real kind — Phase 10.
