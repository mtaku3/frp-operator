package v1alpha1

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSchedulingPolicyRoundTrip(t *testing.T) {
	original := SchedulingPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: SchedulingPolicySpec{
			Allocator:   AllocatorCapacityAware,
			Provisioner: ProvisionerOnDemand,
			Budget: BudgetSpec{
				MaxExits:             ptrInt32(5),
				MaxExitsPerNamespace: ptrInt32(2),
			},
			VPS: VPSSpec{
				Default: VPSDefaults{
					Provider: ProviderDigitalOcean,
					Regions:  []string{"nyc1", "sfo3"},
					Size:     "s-1vcpu-1gb",
					Capacity: &ExitCapacity{
						MaxTunnels:       ptrInt32(50),
						MonthlyTrafficGB: ptrInt64(1000),
						BandwidthMbps:    ptrInt32(1000),
					},
				},
			},
			Consolidation: ConsolidationSpec{
				ReclaimEmpty: true,
				DrainAfter:   metav1.Duration{Duration: 10 * 60 * 1_000_000_000}, // 10m as ns
			},
			Probes: ProbesSpec{
				AdminInterval:    metav1.Duration{Duration: 30 * 1_000_000_000},
				ProviderInterval: metav1.Duration{Duration: 5 * 60 * 1_000_000_000},
				DegradedTimeout:  metav1.Duration{Duration: 5 * 60 * 1_000_000_000},
				LostGracePeriod:  metav1.Duration{Duration: 5 * 60 * 1_000_000_000},
			},
		},
	}
	b, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SchedulingPolicy
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Spec.Allocator != AllocatorCapacityAware {
		t.Errorf("allocator mismatch: %q", got.Spec.Allocator)
	}
	if !got.Spec.Consolidation.ReclaimEmpty {
		t.Errorf("reclaimEmpty not preserved")
	}
	if got.Spec.Probes.AdminInterval.Duration.Seconds() != 30 {
		t.Errorf("adminInterval mismatch: %v", got.Spec.Probes.AdminInterval)
	}
}
