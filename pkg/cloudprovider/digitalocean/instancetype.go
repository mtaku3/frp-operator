package digitalocean

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

// SupportedRegions enumerates the DO regions the operator knows about.
// Subset for v1; extend as needed. Pricing is per-droplet hourly and
// (for the supported sizes) does not vary by region today, so a single
// price per size is sufficient. If DO ever introduces regional pricing,
// extend instanceTypeFor to multiply by a per-region factor.
var SupportedRegions = []string{
	"nyc1", "nyc3", "sfo2", "sfo3", "ams3", "fra1", "lon1",
	"sgp1", "tor1", "blr1", "syd1",
}

// sizeSpec is the static cost/capacity facts for a droplet size.
type sizeSpec struct {
	slug    string
	cpu     string
	mem     string
	bwMbps  string
	priceHr float64
}

var sizeCatalog = []sizeSpec{
	{"s-1vcpu-1gb", "1", "1Gi", "1000", 0.00744},
	{"s-2vcpu-2gb", "2", "2Gi", "2000", 0.01786},
	{"s-2vcpu-4gb", "2", "4Gi", "4000", 0.02976},
}

// InstanceTypes returns the full operator catalog: one entry per droplet
// size, each carrying one Offering per supported region. Karpenter
// scheduler picks the cheapest valid (size, region) offering and pins
// node.kubernetes.io/instance-type + topology.kubernetes.io/region on
// the resulting claim.
func InstanceTypes() []*cloudprovider.InstanceType {
	out := make([]*cloudprovider.InstanceType, 0, len(sizeCatalog))
	for _, s := range sizeCatalog {
		out = append(out, instanceTypeFor(s, SupportedRegions))
	}
	return out
}

// FilteredInstanceTypes narrows the catalog to a ProviderClass discovery
// set: Sizes ∩ catalog, Offerings filtered to Regions ∩ supported.
func FilteredInstanceTypes(allowedSizes, allowedRegions []string) []*cloudprovider.InstanceType {
	regions := intersect(SupportedRegions, allowedRegions)
	if len(regions) == 0 {
		return nil
	}
	out := make([]*cloudprovider.InstanceType, 0, len(sizeCatalog))
	for _, s := range sizeCatalog {
		if !contains(allowedSizes, s.slug) {
			continue
		}
		out = append(out, instanceTypeFor(s, regions))
	}
	return out
}

func instanceTypeFor(s sizeSpec, regions []string) *cloudprovider.InstanceType {
	offerings := make(cloudprovider.Offerings, 0, len(regions))
	for _, r := range regions {
		offerings = append(offerings, &cloudprovider.Offering{
			Available: true,
			Price:     s.priceHr,
			Requirements: []v1alpha1.NodeSelectorRequirementWithMinValues{
				{Key: v1alpha1.RequirementRegion, Operator: v1alpha1.NodeSelectorOpIn, Values: []string{r}},
				{Key: v1alpha1.RequirementInstanceType, Operator: v1alpha1.NodeSelectorOpIn, Values: []string{s.slug}},
			},
		})
	}
	return &cloudprovider.InstanceType{
		Name: s.slug,
		Requirements: []v1alpha1.NodeSelectorRequirementWithMinValues{
			{Key: v1alpha1.RequirementInstanceType, Operator: v1alpha1.NodeSelectorOpIn, Values: []string{s.slug}},
		},
		Offerings: offerings,
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(s.cpu),
			corev1.ResourceMemory: resource.MustParse(s.mem),
			corev1.ResourceName(v1alpha1.ResourceBandwidthMbps): resource.MustParse(s.bwMbps),
		},
		Overhead: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func intersect(a, b []string) []string {
	if len(b) == 0 {
		return nil
	}
	out := make([]string, 0, len(a))
	for _, x := range a {
		if contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}
