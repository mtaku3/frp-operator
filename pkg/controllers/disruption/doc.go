// Package disruption polls the cluster cache, evaluates pluggable Methods
// (Emptiness, Drift, Expiration, MultiNodeConsolidation,
// SingleNodeConsolidation) against per-pool budgets, and enqueues
// disruption commands. The Queue marks candidates for deletion in-memory
// (gating the provisioner), launches replacement claims if needed, waits
// for replacements to reach Ready, then triggers ExitClaim deletion;
// the lifecycle finalizer takes over from there.
package disruption
