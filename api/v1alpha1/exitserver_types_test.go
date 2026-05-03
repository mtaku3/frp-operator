package v1alpha1

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExitServerRoundTrip(t *testing.T) {
	original := ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "exit-1", Namespace: "default"},
		Spec: ExitServerSpec{
			Provider:       ProviderDigitalOcean,
			Region:         "nyc1",
			Size:           "s-1vcpu-1gb",
			CredentialsRef: SecretKeyRef{Name: "do-token", Key: "token"},
			SSH:            SSHConfig{Port: 22},
			Frps: FrpsConfig{
				Version:   "v0.65.0",
				BindPort:  7000,
				AdminPort: 7500,
			},
			AllowPorts:    []string{"1024-65535"},
			ReservedPorts: []int32{22, 7000, 7500},
			Capacity: &ExitCapacity{
				MaxTunnels:       ptrInt32(50),
				MonthlyTrafficGB: ptrInt64(1000),
				BandwidthMbps:    ptrInt32(1000),
			},
		},
		Status: ExitServerStatus{
			Phase:       PhaseReady,
			PublicIP:    "203.0.113.10",
			ProviderID:  "do-droplet-123456",
			FrpsVersion: "v0.65.0",
			Allocations: map[string]string{"443": "ns/foo"},
			Usage: ExitUsage{
				Tunnels:          1,
				MonthlyTrafficGB: 100,
				BandwidthMbps:    200,
			},
		},
	}
	b, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ExitServer
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Spec.Provider != original.Spec.Provider {
		t.Errorf("provider mismatch: got %q want %q", got.Spec.Provider, original.Spec.Provider)
	}
	if got.Status.Allocations["443"] != "ns/foo" {
		t.Errorf("allocations not preserved: %#v", got.Status.Allocations)
	}
	if *got.Spec.Capacity.MaxTunnels != 50 {
		t.Errorf("capacity.maxTunnels not preserved")
	}
}

func TestExitServerStatusDrainStartedAtRoundTrip(t *testing.T) {
	now := metav1.Now()
	original := ExitServer{
		ObjectMeta: metav1.ObjectMeta{Name: "e", Namespace: "default"},
		Spec: ExitServerSpec{
			Provider:   ProviderDigitalOcean,
			Frps:       FrpsConfig{Version: "v0.68.1"},
			AllowPorts: []string{"1024-65535"},
		},
		Status: ExitServerStatus{
			Phase:          PhaseDraining,
			DrainStartedAt: &now,
		},
	}
	b, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ExitServer
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Status.DrainStartedAt == nil {
		t.Fatal("DrainStartedAt lost in round-trip")
	}
	// JSON marshaling of metav1.Time truncates to seconds precision,
	// so compare Unix timestamps rather than exact time values.
	if got.Status.DrainStartedAt.Unix() != now.Unix() {
		t.Errorf("DrainStartedAt mismatch: got %v want %v", got.Status.DrainStartedAt, &now)
	}
}

func ptrInt32(v int32) *int32 { return &v }
func ptrInt64(v int64) *int64 { return &v }
