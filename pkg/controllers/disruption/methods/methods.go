package methods

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// DefaultMethods returns the canonical disruption Method ordering:
// Emptiness → Drift → Expiration → MultiNodeConsolidation →
// SingleNodeConsolidation. Matches Karpenter's priority order; the
// controller stops after the first method that fires per reconcile.
//
// Karpenter has no "static drift" method — pool template metadata
// changes silently propagate to new claims; existing claims keep their
// at-creation labels until rebuild. This operator follows that
// convention.
func DefaultMethods(cluster *state.Cluster, kube client.Client) []disruption.Method {
	sim := NewSimulator(cluster, kube)
	return []disruption.Method{
		NewEmptiness(),
		NewDrift(),
		NewExpiration(),
		NewMultiNodeConsolidation(sim),
		NewSingleNodeConsolidation(sim),
	}
}
