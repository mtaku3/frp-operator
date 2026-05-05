package digitalocean_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean"
)

func TestRenderCloudInit_DefaultTemplate(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AllowPorts: []string{"80"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
	}
	out, err := digitalocean.RenderCloudInit(cfg, "tok", "")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "#cloud-config\n"))
	require.Contains(t, out, "https://github.com/fatedier/frp/releases/download/v0.68.1/frp_0.68.1_linux_amd64.tar.gz")
	require.Contains(t, out, "ExecStart=/usr/local/bin/frps -c /etc/frp/frps.toml")
	require.Contains(t, out, "/etc/systemd/system/frps.service")
	require.Contains(t, out, "bindPort = 7000")
	require.Contains(t, out, `token = "tok"`)
}

func TestRenderCloudInit_OverridesNonURLTemplate(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AllowPorts: []string{"80"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
	}
	// pc.Spec.DefaultImage default is the historic "fatedier/frps:%s"
	// container ref. Cloud-init falls back to the binary URL template.
	out, err := digitalocean.RenderCloudInit(cfg, "t", "fatedier/frps:%s")
	require.NoError(t, err)
	require.Contains(t, out, "https://github.com/fatedier/frp/releases/download/v0.68.1/frp_0.68.1_linux_amd64.tar.gz")
}

func TestRenderCloudInit_CustomURLTemplate(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AllowPorts: []string{"80"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
	}
	out, err := digitalocean.RenderCloudInit(cfg, "t", "https://example.com/frp/%s/frp_%s.tar.gz")
	require.NoError(t, err)
	require.Contains(t, out, "https://example.com/frp/v0.68.1/frp_0.68.1.tar.gz")
}
