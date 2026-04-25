package controller

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func TestNextTunnelPhase(t *testing.T) {
	cases := []struct {
		name                    string
		current                 frpv1alpha1.TunnelPhase
		exitAssigned, exitReady, frpcReady bool
		want                    frpv1alpha1.TunnelPhase
	}{
		{"no exit yet", "", false, false, false, frpv1alpha1.TunnelAllocating},
		{"exit assigned but provisioning", frpv1alpha1.TunnelAllocating, true, false, false, frpv1alpha1.TunnelProvisioning},
		{"exit ready, frpc connecting", frpv1alpha1.TunnelProvisioning, true, true, false, frpv1alpha1.TunnelConnecting},
		{"all ready", frpv1alpha1.TunnelConnecting, true, true, true, frpv1alpha1.TunnelReady},
		{"ready loses frpc -> Disconnected", frpv1alpha1.TunnelReady, true, true, false, frpv1alpha1.TunnelDisconnected},
		{"disconnected recovers", frpv1alpha1.TunnelDisconnected, true, true, true, frpv1alpha1.TunnelReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextTunnelPhase(tc.current, tc.exitAssigned, tc.exitReady, tc.frpcReady)
			if got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
