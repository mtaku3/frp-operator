package fake_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
)

func newClaim(name string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ExitClaimSpec{
			Frps: v1alpha1.FrpsConfig{
				Version:    "v0.68.1",
				BindPort:   7000,
				AllowPorts: []string{"80"},
				Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
			},
		},
	}
}

func TestFake_CreateGetDeleteRoundtrip(t *testing.T) {
	ctx := context.Background()
	cp := fake.New()
	in := newClaim("e1")

	out, err := cp.Create(ctx, in)
	require.NoError(t, err)
	require.NotEmpty(t, out.Status.ProviderID)
	require.Equal(t, "fake-exit-e1", out.Status.ExitName)

	got, err := cp.Get(ctx, out.Status.ProviderID)
	require.NoError(t, err)
	require.Equal(t, out.Status.ExitName, got.Status.ExitName)

	require.NoError(t, cp.Delete(ctx, out))
	_, err = cp.Get(ctx, out.Status.ProviderID)
	require.True(t, cloudprovider.IsExitNotFound(err))
}

func TestFake_Idempotent(t *testing.T) {
	ctx := context.Background()
	cp := fake.New()
	a, err := cp.Create(ctx, newClaim("x"))
	require.NoError(t, err)
	b, err := cp.Create(ctx, newClaim("x"))
	require.NoError(t, err)
	require.Equal(t, a.Status.ProviderID, b.Status.ProviderID)
}

func TestFake_InjectedFailure(t *testing.T) {
	ctx := context.Background()
	cp := fake.New()
	cp.ErrorOnCreate("simulated outage")
	_, err := cp.Create(ctx, newClaim("y"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "simulated outage")
}

func TestFake_List(t *testing.T) {
	ctx := context.Background()
	cp := fake.New()
	_, _ = cp.Create(ctx, newClaim("a"))
	_, _ = cp.Create(ctx, newClaim("b"))
	out, err := cp.List(ctx)
	require.NoError(t, err)
	require.Len(t, out, 2)
}

func TestFake_GetSupportedProviderClasses(t *testing.T) {
	cp := fake.New()
	classes := cp.GetSupportedProviderClasses()
	require.Len(t, classes, 1)
	_, ok := classes[0].(*fake.FakeProviderClass)
	require.True(t, ok)
}
