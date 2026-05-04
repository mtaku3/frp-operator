package digitalocean

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// InstanceTypes is the static catalog of DO sizes recognized by the
// operator. Subset for v1; expand as needed. See https://slugs.do-api.dev/.
func InstanceTypes() []*cloudprovider.InstanceType {
	mk := func(slug string, cpu, mem, bwMbps string, price float64) *cloudprovider.InstanceType {
		return &cloudprovider.InstanceType{
			Name: slug,
			Requirements: []v1alpha1.NodeSelectorRequirementWithMinValues{
				{Key: "node.kubernetes.io/instance-type", Operator: v1alpha1.NodeSelectorOpIn, Values: []string{slug}},
			},
			Offerings: cloudprovider.Offerings{{Available: true, Price: price}},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(mem),
				corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse(bwMbps),
			},
			Overhead: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		}
	}
	return []*cloudprovider.InstanceType{
		mk("s-1vcpu-1gb", "1", "1Gi", "1000", 0.00744),
		mk("s-2vcpu-2gb", "2", "2Gi", "2000", 0.01786),
		mk("s-2vcpu-4gb", "2", "4Gi", "4000", 0.02976),
	}
}
