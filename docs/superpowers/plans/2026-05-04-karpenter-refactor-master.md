# Karpenter-Style Refactor — Master Plan

> **For agentic workers:** This is a master decomposition document, not a single executable plan. Each phase below has its own dedicated plan file. Execute phase-by-phase.

**Spec:** `docs/superpowers/specs/2026-05-04-karpenter-style-refactor-design.md`

**Goal:** Rewrite the operator's controllers/scheduler/provider layer to mirror `sigs.k8s.io/karpenter`'s architecture (singleton scheduler with batcher, lifecycle controller, disruption controller, shared state.Cluster, no admission webhooks).

**Strategy:** Big-bang on `karpenter-refactor` branch. Each phase ends with green tests so we can ship intermediate progress and validate the architecture incrementally.

---

## Phase decomposition

| # | Phase | End state | Plan file |
|---|-------|-----------|-----------|
| 1 | API foundations | New CRD types compile, codegen produces manifests, no controller logic. | `2026-05-04-phase-1-api-foundations.md` (this PR) |
| 2 | CloudProvider interface + fake | New `pkg/cloudprovider` interface, in-memory fake, ported localdocker + DO. Unit tests pass. | `phase-2-cloudprovider.md` |
| 3 | state.Cluster + informer controllers | In-memory cluster cache, write-only informer controllers, `Synced(ctx)` gate. Integration tests via envtest. | `phase-3-state-cluster.md` |
| 4 | Provisioning loop (singleton + batcher + scheduler) | Tunnels get bound to ExitClaims (or NewClaim records emitted). No actual provider Create yet. | `phase-4-provisioning.md` |
| 5 | ExitClaim lifecycle controller | Launch → Register → Initialize → Liveness phases. Real frps containers/droplets come up. | `phase-5-lifecycle.md` |
| 6 | Disruption controller + Methods + budgets | Emptiness, Drift, Expiration, Consolidation, all gated by budgets. | `phase-6-disruption.md` |
| 7 | ExitPool ancillary controllers | counter, hash, validation, readiness — Pool.Status surfaces. | `phase-7-exitpool-status.md` |
| 8 | ServiceWatcher rewrite (annotations) | Service → Tunnel translation via annotations contract. | `phase-8-servicewatcher.md` |
| 9 | Operator wiring (drop webhooks, add health/metrics) | Webhook server gone, CEL on CRDs, healthz/readyz, metrics catalog. | `phase-9-operator-wiring.md` |
| 10 | E2E port | Rewrite `test/e2e/*` against new CRDs. Full suite green on kind + localdocker. | `phase-10-e2e.md` |

Each phase is **independently mergeable** to `karpenter-refactor`. Phase boundaries chosen so each one ends compile-clean + unit-tests-green. Phase 10 is the only e2e gate.

---

## Cross-cutting principles

- **No backwards compat.** This is a rewrite. Old `Tunnel/ExitServer/SchedulingPolicy` (current v1alpha1) get deleted before new `Tunnel/ExitClaim/ExitPool/*ProviderClass` (new v1alpha1) land. CHANGELOG announces.
- **TDD throughout.** Every controller package has `suite_test.go` + envtest. Every scheduler package has unit tests against fake data structures.
- **Frequent commits.** One conceptual change per commit. Branch is shared so commits should pass `go vet` and `make test`.
- **No partial migrations.** A phase either fully replaces the old subsystem or is not merged. No co-existence.

---

## Phase 1 plan

See `2026-05-04-phase-1-api-foundations.md`.
