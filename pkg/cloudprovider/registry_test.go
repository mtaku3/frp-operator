package cloudprovider_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
)

func TestRegistry_For_Found(t *testing.T) {
	reg := cloudprovider.NewRegistry()
	fp := fake.New()
	require.NoError(t, reg.Register("LocalDockerProviderClass", fp))

	got, err := reg.For("LocalDockerProviderClass")
	require.NoError(t, err)
	require.Same(t, fp, got)
}

func TestRegistry_For_Unknown(t *testing.T) {
	reg := cloudprovider.NewRegistry()
	_, err := reg.For("DoesNotExist")
	require.Error(t, err)
}

func TestRegistry_DoubleRegister_Errors(t *testing.T) {
	reg := cloudprovider.NewRegistry()
	require.NoError(t, reg.Register("X", fake.New()))
	require.Error(t, reg.Register("X", fake.New())) // explicit error to avoid silent override
}
