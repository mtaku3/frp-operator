// Package operator wires every controller from Phases 3-8 into a single
// controller-runtime manager. Run(ctx, *Config) is the entrypoint used by
// cmd/manager.
//
// No admission webhooks are registered; CEL on the CRDs is the only
// validation surface (per spec §10).
package operator
