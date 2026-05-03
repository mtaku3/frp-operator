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
	// Reclaim controller owns Draining; don't overwrite it with Ready or
	// Provisioning when the provider is still running. Lost (gone/failed)
	// still wins because the container is actually gone.
	if current == frpv1alpha1.PhaseDraining {
		switch providerState {
		case provider.PhaseGone, provider.PhaseFailed:
			return frpv1alpha1.PhaseLost
		}
		return frpv1alpha1.PhaseDraining
	}
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
