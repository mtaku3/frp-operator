package controller

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/internal/provider"
)

func TestNextPhase(t *testing.T) {
	type input struct {
		current     frpv1alpha1.ExitPhase
		providerSt  provider.Phase
		adminOK     bool // last admin-API probe outcome
	}
	cases := []struct {
		name string
		in   input
		want frpv1alpha1.ExitPhase
	}{
		{
			name: "fresh CR with no provider state yet",
			in:   input{current: "", providerSt: "", adminOK: false},
			want: frpv1alpha1.PhasePending,
		},
		{
			name: "provider says provisioning",
			in:   input{current: frpv1alpha1.PhasePending, providerSt: provider.PhaseProvisioning, adminOK: false},
			want: frpv1alpha1.PhaseProvisioning,
		},
		{
			name: "provider running, admin not yet OK",
			in:   input{current: frpv1alpha1.PhaseProvisioning, providerSt: provider.PhaseRunning, adminOK: false},
			want: frpv1alpha1.PhaseProvisioning, // still bootstrapping; admin not up
		},
		{
			name: "provider running and admin OK -> Ready",
			in:   input{current: frpv1alpha1.PhaseProvisioning, providerSt: provider.PhaseRunning, adminOK: true},
			want: frpv1alpha1.PhaseReady,
		},
		{
			name: "ready exit fails admin probe -> Degraded",
			in:   input{current: frpv1alpha1.PhaseReady, providerSt: provider.PhaseRunning, adminOK: false},
			want: frpv1alpha1.PhaseDegraded,
		},
		{
			name: "degraded exit recovers admin -> Ready",
			in:   input{current: frpv1alpha1.PhaseDegraded, providerSt: provider.PhaseRunning, adminOK: true},
			want: frpv1alpha1.PhaseReady,
		},
		{
			name: "provider reports Gone -> Lost",
			in:   input{current: frpv1alpha1.PhaseReady, providerSt: provider.PhaseGone, adminOK: false},
			want: frpv1alpha1.PhaseLost,
		},
		{
			name: "provider reports Failed -> Lost (no recovery without manual intervention)",
			in:   input{current: frpv1alpha1.PhaseProvisioning, providerSt: provider.PhaseFailed, adminOK: false},
			want: frpv1alpha1.PhaseLost,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextPhase(tc.in.current, tc.in.providerSt, tc.in.adminOK)
			if got != tc.want {
				t.Errorf("nextPhase(%v, %v, %v) = %v, want %v",
					tc.in.current, tc.in.providerSt, tc.in.adminOK, got, tc.want)
			}
		})
	}
}
