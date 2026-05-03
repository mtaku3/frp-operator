package scheduler

import (
	"testing"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func makeExit(name string, opts ...func(*frpv1alpha1.ExitServer)) frpv1alpha1.ExitServer {
	e := frpv1alpha1.ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: frpv1alpha1.ExitServerSpec{
			Provider: frpv1alpha1.ProviderDigitalOcean,
			Region:   "nyc1",
		},
		Status: frpv1alpha1.ExitServerStatus{
			Phase:       frpv1alpha1.PhaseReady,
			Allocations: map[string]string{},
			Usage:       frpv1alpha1.ExitUsage{},
		},
	}
	for _, o := range opts {
		o(&e)
	}
	return e
}

func TestPortsFitWithReserved(t *testing.T) {
	exit := makeExit("e", func(e *frpv1alpha1.ExitServer) {
		e.Spec.ReservedPorts = []int32{22, 7000, 7500}
		e.Status.Allocations = map[string]string{"443": "ns/used"}
	})
	cases := []struct {
		name  string
		ports []int32
		fit   bool
	}{
		{"all free", []int32{80}, true},
		{"one already allocated", []int32{443, 80}, false},
		{"reserved port", []int32{22, 80}, false},
		{"empty ports trivially fit", []int32{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PortsFit(exit, tc.ports)
			if got != tc.fit {
				t.Errorf("got %v want %v", got, tc.fit)
			}
		})
	}
}

func TestCapacityFits(t *testing.T) {
	mt := int32(10)
	mtg := int64(100)
	bw := int32(500)
	exit := makeExit("e", func(e *frpv1alpha1.ExitServer) {
		e.Spec.Capacity = &frpv1alpha1.ExitCapacity{
			MaxTunnels: &mt, MonthlyTrafficGB: &mtg, BandwidthMbps: &bw,
		}
		e.Status.Usage = frpv1alpha1.ExitUsage{
			Tunnels: 9, MonthlyTrafficGB: 80, BandwidthMbps: 400,
		}
	})
	cases := []struct {
		name string
		req  frpv1alpha1.TunnelRequirements
		fit  bool
	}{
		{"empty req fits", frpv1alpha1.TunnelRequirements{}, true},
		{
			"one more tunnel fits exactly",
			frpv1alpha1.TunnelRequirements{
				MonthlyTrafficGB: ptrInt64(20), BandwidthMbps: ptrInt32(100),
			},
			true,
		},
		{
			"would exceed traffic",
			frpv1alpha1.TunnelRequirements{
				MonthlyTrafficGB: ptrInt64(21),
			},
			false,
		},
		{
			"would exceed bandwidth",
			frpv1alpha1.TunnelRequirements{
				BandwidthMbps: ptrInt32(101),
			},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CapacityFits(exit, tc.req)
			if got != tc.fit {
				t.Errorf("got %v want %v", got, tc.fit)
			}
		})
	}
}

func TestPlacementMatches(t *testing.T) {
	exit := makeExit("e")
	cases := []struct {
		name      string
		placement *frpv1alpha1.Placement
		match     bool
	}{
		{"nil placement matches anything", nil, true},
		{"matching provider", &frpv1alpha1.Placement{
			Providers: []frpv1alpha1.Provider{frpv1alpha1.ProviderDigitalOcean},
		}, true},
		{"non-matching provider", &frpv1alpha1.Placement{
			Providers: []frpv1alpha1.Provider{frpv1alpha1.ProviderLocalDocker},
		}, false},
		{"matching region", &frpv1alpha1.Placement{
			Regions: []string{"nyc1", "sfo3"},
		}, true},
		{"non-matching region", &frpv1alpha1.Placement{
			Regions: []string{"sfo3"},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlacementMatches(exit, tc.placement)
			if got != tc.match {
				t.Errorf("got %v want %v", got, tc.match)
			}
		})
	}
}

func TestEligibleExits(t *testing.T) {
	mt := int32(50)
	exits := []frpv1alpha1.ExitServer{
		makeExit("ready"),
		makeExit("provisioning", func(e *frpv1alpha1.ExitServer) { e.Status.Phase = frpv1alpha1.PhaseProvisioning }),
		makeExit("draining", func(e *frpv1alpha1.ExitServer) { e.Status.Phase = frpv1alpha1.PhaseDraining }),
		makeExit("at-cap", func(e *frpv1alpha1.ExitServer) {
			e.Spec.Capacity = &frpv1alpha1.ExitCapacity{MaxTunnels: &mt}
			e.Status.Usage.Tunnels = 50
		}),
	}
	tunnel := &frpv1alpha1.Tunnel{
		Spec: frpv1alpha1.TunnelSpec{
			Ports: []frpv1alpha1.TunnelPort{{Name: "h", ServicePort: 80}},
		},
	}
	got := EligibleExits(exits, tunnel)
	if len(got) != 1 || got[0].Name != "ready" {
		names := make([]string, 0, len(got))
		for _, e := range got {
			names = append(names, e.Name)
		}
		t.Errorf("expected only [ready]; got %v", names)
	}
}

// helpers — note these helpers are NEW for this package and do NOT collide
// with the api/v1alpha1 ptrInt32/ptrInt64 (different package).
func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }
