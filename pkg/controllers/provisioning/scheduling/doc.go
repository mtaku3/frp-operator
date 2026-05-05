// Package scheduling implements the three-stage Solve pipeline used by
// the provisioner: addToExistingExit -> addToInflightClaim -> addToNewClaim.
// Mirrors sigs.k8s.io/karpenter pkg/controllers/provisioning/scheduling.
//
// All predicates in this package are pure functions over plain Go data so
// the scheduler can be exhaustively tested without envtest or a manager.
package scheduling
