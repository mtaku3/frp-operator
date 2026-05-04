package localdocker

import (
	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

// ContainerNameForTest exposes containerName for unit tests.
func ContainerNameForTest(claim *v1alpha1.ExitClaim) string {
	return containerName(claim)
}

// ImageRefForTest exposes imageRef for unit tests.
func ImageRefForTest(pc *ldv1alpha1.LocalDockerProviderClass, version string) string {
	return imageRef(pc, version)
}

// SanitizeNameForTest exposes sanitizeName for unit tests.
func SanitizeNameForTest(s string) string {
	return sanitizeName(s)
}
