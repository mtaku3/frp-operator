package methods

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/mtaku3/frp-operator/pkg/controllers/disruption"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

// DefaultMethods returns the canonical disruption Method ordering:
// Emptiness → StaticDrift → Drift → Expiration → MultiNodeConsolidation →
// SingleNodeConsolidation. This matches Karpenter's priority order; the
// controller stops after the first method that fires per reconcile.
//
// Phase 9 wiring calls this from the manager bootstrap so the ordering lives
// in one place.
func DefaultMethods(cluster *state.Cluster, kube client.Client) []disruption.Method {
	sim := NewSimulator(cluster, kube)
	return []disruption.Method{
		NewEmptiness(),
		NewStaticDrift(),
		NewDrift(),
		NewExpiration(),
		NewMultiNodeConsolidation(sim),
		NewSingleNodeConsolidation(sim),
	}
}
