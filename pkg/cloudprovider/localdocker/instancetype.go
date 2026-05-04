package localdocker

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// InstanceTypes returns the localdocker instance-type catalog. There's
// only one shape with effectively unbounded capacity; localdocker has
// no cost or quota dimensions.
func InstanceTypes() []*cloudprovider.InstanceType {
	return []*cloudprovider.InstanceType{
		{
			Name: "local-1",
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
				corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse("10000"),
			},
			Offerings: cloudprovider.Offerings{{Available: true, Price: 0}},
		},
	}
}
