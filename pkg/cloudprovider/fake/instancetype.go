package fake

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// DefaultInstanceTypes returns the catalog every fake-backed test sees
// unless overridden.
func DefaultInstanceTypes() []*cloudprovider.InstanceType {
	return []*cloudprovider.InstanceType{
		{
			Name: "fake-small",
			Requirements: []v1alpha1.NodeSelectorRequirementWithMinValues{
				{Key: "frp.operator.io/region", Operator: v1alpha1.NodeSelectorOpIn, Values: []string{"fake-region-1"}},
			},
			Offerings: cloudprovider.Offerings{
				{Requirements: nil, Price: 0, Available: true},
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
				corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse("1000"),
			},
			Overhead: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	}
}
