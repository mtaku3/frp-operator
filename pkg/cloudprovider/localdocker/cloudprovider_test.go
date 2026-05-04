package localdocker_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

func TestContainerName_Deterministic(t *testing.T) {
	a := localdocker.ContainerNameForTest(&v1alpha1.ExitClaim{ObjectMeta: metav1.ObjectMeta{Name: "exit-a"}})
	b := localdocker.ContainerNameForTest(&v1alpha1.ExitClaim{ObjectMeta: metav1.ObjectMeta{Name: "exit-a"}})
	require.Equal(t, a, b)
	require.Equal(t, "frp-operator-exit-a", a)
}

func TestContainerName_DifferentForDifferentInputs(t *testing.T) {
	a := localdocker.ContainerNameForTest(&v1alpha1.ExitClaim{ObjectMeta: metav1.ObjectMeta{Name: "exit-a"}})
	b := localdocker.ContainerNameForTest(&v1alpha1.ExitClaim{ObjectMeta: metav1.ObjectMeta{Name: "exit-b"}})
	require.NotEqual(t, a, b)
}

func TestSanitizeName(t *testing.T) {
	require.Equal(t, "abc", localdocker.SanitizeNameForTest("abc"))
	require.Equal(t, "ab_c", localdocker.SanitizeNameForTest("ab/c"))
	require.Equal(t, "x", localdocker.SanitizeNameForTest(""))
	// First char non-alnum-non-underscore gets prefixed with x.
	require.Equal(t, "x.dot", localdocker.SanitizeNameForTest(".dot"))
}

func TestImageRef_DefaultTemplate(t *testing.T) {
	pc := &ldv1alpha1.LocalDockerProviderClass{
		Spec: ldv1alpha1.LocalDockerProviderClassSpec{DefaultImage: "fatedier/frps:%s"},
	}
	require.Equal(t, "fatedier/frps:v0.68.1", localdocker.ImageRefForTest(pc, "v0.68.1"))
}

func TestImageRef_EmptyDefault(t *testing.T) {
	pc := &ldv1alpha1.LocalDockerProviderClass{}
	require.Equal(t, "fatedier/frps:v0.68.1", localdocker.ImageRefForTest(pc, "v0.68.1"))
}

func TestImageRef_NoSubstitution(t *testing.T) {
	pc := &ldv1alpha1.LocalDockerProviderClass{
		Spec: ldv1alpha1.LocalDockerProviderClassSpec{DefaultImage: "myreg/frps:fixed"},
	}
	require.Equal(t, "myreg/frps:fixed", localdocker.ImageRefForTest(pc, "v0.68.1"))
}

func TestInstanceTypes(t *testing.T) {
	its := localdocker.InstanceTypes()
	require.Len(t, its, 1)
	require.Equal(t, "local-1", its[0].Name)
	require.NotEmpty(t, its[0].Capacity)
}

func TestGetSupportedProviderClasses(t *testing.T) {
	cp := &localdocker.CloudProvider{}
	classes := cp.GetSupportedProviderClasses()
	require.Len(t, classes, 1)
	_, ok := classes[0].(*ldv1alpha1.LocalDockerProviderClass)
	require.True(t, ok)
}

func TestResolveClass_RefusesWrongKind(t *testing.T) {
	cp := &localdocker.CloudProvider{}
	claim := &v1alpha1.ExitClaim{
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef: v1alpha1.ProviderClassRef{Kind: "DigitalOceanProviderClass", Name: "x"},
		},
	}
	_, err := cp.Create(t.Context(), claim)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing kind")
}
