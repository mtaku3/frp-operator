package cloudprovider_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/fake"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker"
)

// TestRegistry_AllProvidersRegister wires every provider construct path
// into the Registry. Real backends are NOT contacted; this exercises
// only construction + GetSupportedProviderClasses + Registry.Register.
func TestRegistry_AllProvidersRegister(t *testing.T) {
	reg := cloudprovider.NewRegistry()

	// Fake always works.
	fp := fake.New()
	require.NoError(t, reg.Register("FakeProviderClass", fp))

	// Localdocker construction may fail if no Docker socket is available;
	// register only when New succeeds. Either way the registry stays
	// usable.
	if cp, err := localdocker.New(nil); err == nil {
		require.NoError(t, reg.Register("LocalDockerProviderClass", cp))
		_ = cp.Close()
	}

	// DigitalOcean construction never makes a network call.
	doCP, err := digitalocean.New(nil, "")
	require.NoError(t, err)
	require.NoError(t, reg.Register("DigitalOceanProviderClass", doCP))

	got, err := reg.For("FakeProviderClass")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Same(t, fp, got)

	got2, err := reg.For("DigitalOceanProviderClass")
	require.NoError(t, err)
	require.Equal(t, "digital-ocean", got2.Name())
	require.Len(t, got2.GetSupportedProviderClasses(), 1)
}
