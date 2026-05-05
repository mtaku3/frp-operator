// Package exitpool hosts the small, single-responsibility controllers that
// drive ExitPool.Status surfaces:
//
//   - hash:       computes Spec.Template.Spec hash, stamps Pool +
//                 child ExitClaim annotations (drift signal for Phase 6)
//   - counter:    rolls up child ExitClaim resources into Pool.Status
//   - readiness:  resolves Pool.Spec.Template.Spec.ProviderClassRef and
//                 sets Conditions[ProviderClassReady, Ready]
//   - validation: surfaces stateful spec validation as
//                 Conditions[ValidationSucceeded]
//
// Each sub-package is independent: own controller, own envtest suite, no
// shared state between them. Phase 9 wires them all into the manager at
// startup.
package exitpool
