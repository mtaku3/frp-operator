package controller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

func mkSvc(opts ...func(*corev1.Service)) *corev1.Service {
	s := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "svc1", Namespace: "ns"},
		Spec: corev1.ServiceSpec{
			Type:              corev1.ServiceTypeLoadBalancer,
			LoadBalancerClass: ptr.To("frp-operator.io/frp"),
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
			},
		},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func TestTranslateServiceToTunnelSpec(t *testing.T) {
	cases := []struct {
		name    string
		svc     *corev1.Service
		want    frpv1alpha1.TunnelSpec
		wantErr bool
	}{
		{
			name: "minimal LoadBalancer with one port",
			svc:  mkSvc(),
			want: frpv1alpha1.TunnelSpec{
				Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
				},
			},
		},
		{
			name: "exit annotation hard-pins exitRef",
			svc: mkSvc(func(s *corev1.Service) {
				s.Annotations = map[string]string{"frp-operator.io/exit": "exit-nyc-1"}
			}),
			want: frpv1alpha1.TunnelSpec{
				Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
				ExitRef: &frpv1alpha1.ExitRef{Name: "exit-nyc-1"},
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
				},
			},
		},
		{
			name: "provider/region/size annotations populate placement",
			svc: mkSvc(func(s *corev1.Service) {
				s.Annotations = map[string]string{
					"frp-operator.io/provider": "digitalocean",
					"frp-operator.io/region":   "nyc1,sfo3",
					"frp-operator.io/size":     "s-2vcpu-2gb",
				}
			}),
			want: frpv1alpha1.TunnelSpec{
				Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
				Placement: &frpv1alpha1.Placement{
					Providers:    []frpv1alpha1.Provider{frpv1alpha1.ProviderDigitalOcean},
					Regions:      []string{"nyc1", "sfo3"},
					SizeOverride: "s-2vcpu-2gb",
				},
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
				},
			},
		},
		{
			name: "scheduling-policy + migration-policy + allow-port-split",
			svc: mkSvc(func(s *corev1.Service) {
				s.Annotations = map[string]string{
					"frp-operator.io/scheduling-policy":  "team-a",
					"frp-operator.io/migration-policy":   "OnExitLost",
					"frp-operator.io/allow-port-split":   "true",
					"frp-operator.io/immutable-when-ready": "true",
				}
			}),
			want: frpv1alpha1.TunnelSpec{
				Service:             frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
				SchedulingPolicyRef: frpv1alpha1.PolicyRef{Name: "team-a"},
				MigrationPolicy:     frpv1alpha1.MigrationOnExitLost,
				AllowPortSplit:      true,
				ImmutableWhenReady:  true,
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
				},
			},
		},
		{
			name: "traffic-gb and bandwidth-mbps annotations populate requirements",
			svc: mkSvc(func(s *corev1.Service) {
				s.Annotations = map[string]string{
					"frp-operator.io/traffic-gb":     "100",
					"frp-operator.io/bandwidth-mbps": "200",
				}
			}),
			want: frpv1alpha1.TunnelSpec{
				Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
				Requirements: &frpv1alpha1.TunnelRequirements{
					MonthlyTrafficGB: ptr.To(int64(100)),
					BandwidthMbps:    ptr.To(int32(200)),
				},
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
				},
			},
		},
		{
			name: "non-numeric traffic-gb errors",
			svc: mkSvc(func(s *corev1.Service) {
				s.Annotations = map[string]string{"frp-operator.io/traffic-gb": "abc"}
			}),
			wantErr: true,
		},
		{
			name: "multi-port Service",
			svc: mkSvc(func(s *corev1.Service) {
				s.Spec.Ports = []corev1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
					{Name: "https", Port: 443, TargetPort: intstr.FromInt(8443), Protocol: corev1.ProtocolTCP},
				}
			}),
			want: frpv1alpha1.TunnelSpec{
				Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "http", ServicePort: 80, PublicPort: ptr.To(int32(80)), Protocol: frpv1alpha1.ProtocolTCP},
					{Name: "https", ServicePort: 443, PublicPort: ptr.To(int32(443)), Protocol: frpv1alpha1.ProtocolTCP},
				},
			},
		},
		{
			name: "UDP port",
			svc: mkSvc(func(s *corev1.Service) {
				s.Spec.Ports = []corev1.ServicePort{
					{Name: "dns", Port: 53, TargetPort: intstr.FromInt(53), Protocol: corev1.ProtocolUDP},
				}
			}),
			want: frpv1alpha1.TunnelSpec{
				Service: frpv1alpha1.ServiceRef{Name: "svc1", Namespace: "ns"},
				Ports: []frpv1alpha1.TunnelPort{
					{Name: "dns", ServicePort: 53, PublicPort: ptr.To(int32(53)), Protocol: frpv1alpha1.ProtocolUDP},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := translateServiceToTunnelSpec(tc.svc)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("translation mismatch\ngot:  %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestServiceMatchesClass(t *testing.T) {
	cases := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{"matching class", mkSvc(), true},
		{"different class", mkSvc(func(s *corev1.Service) { s.Spec.LoadBalancerClass = ptr.To("other") }), false},
		{"no class set", mkSvc(func(s *corev1.Service) { s.Spec.LoadBalancerClass = nil }), false},
		{"not a LoadBalancer Service", mkSvc(func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeClusterIP }), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if serviceMatchesClass(tc.svc) != tc.want {
				t.Errorf("got %v want %v", !tc.want, tc.want)
			}
		})
	}
}
