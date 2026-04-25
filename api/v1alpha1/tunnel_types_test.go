package v1alpha1

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTunnelRoundTrip(t *testing.T) {
	original := Tunnel{
		ObjectMeta: metav1.ObjectMeta{Name: "my-tunnel", Namespace: "my-ns"},
		Spec: TunnelSpec{
			Service: ServiceRef{Name: "my-svc", Namespace: "my-ns"},
			Ports: []TunnelPort{
				{Name: "http", ServicePort: 80, PublicPort: ptrInt32(80), Protocol: ProtocolTCP},
			},
			ExitRef: &ExitRef{Name: "exit-nyc-1"},
			Placement: &Placement{
				Providers:    []Provider{ProviderDigitalOcean},
				Regions:      []string{"nyc1", "sfo3"},
				SizeOverride: "s-2vcpu-2gb",
			},
			SchedulingPolicyRef: PolicyRef{Name: "default"},
			Requirements: &TunnelRequirements{
				MonthlyTrafficGB: ptrInt64(100),
				BandwidthMbps:    ptrInt32(200),
			},
			MigrationPolicy:    MigrationNever,
			AllowPortSplit:     false,
			ImmutableWhenReady: true,
		},
		Status: TunnelStatus{
			Phase:         TunnelReady,
			AssignedExit:  "exit-nyc-1",
			AssignedIP:    "203.0.113.10",
			AssignedPorts: []int32{80},
		},
	}
	b, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Tunnel
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Spec.MigrationPolicy != MigrationNever {
		t.Errorf("migrationPolicy mismatch")
	}
	if !got.Spec.ImmutableWhenReady {
		t.Errorf("immutableWhenReady not preserved")
	}
	if *got.Spec.Ports[0].PublicPort != 80 {
		t.Errorf("publicPort mismatch")
	}
	if got.Status.AssignedIP != "203.0.113.10" {
		t.Errorf("assignedIP mismatch")
	}
}
