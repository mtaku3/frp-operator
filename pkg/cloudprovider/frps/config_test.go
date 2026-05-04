package frps_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps"
)

func TestRenderConfig_Minimal(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AdminPort:  7400,
		AllowPorts: []string{"80", "443", "1024-65535"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
	}
	out, err := frps.RenderConfig(cfg, "secret-token")
	require.NoError(t, err)
	require.Contains(t, out, "bindPort = 7000")
	require.Contains(t, out, "webServer.port = 7400")
	require.Contains(t, out, `auth.method = "token"`)
	require.Contains(t, out, `auth.token = "secret-token"`)
	require.True(t, strings.Contains(out, "allowPorts"))
}

func TestRenderConfig_TLS(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AllowPorts: []string{"443"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
		TLS:        &v1alpha1.FrpsTLSConfig{Force: true},
	}
	out, err := frps.RenderConfig(cfg, "tok")
	require.NoError(t, err)
	require.Contains(t, out, "transport.tls.force = true")
}

func TestRenderConfig_DefaultBindPort(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		AllowPorts: []string{"80"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "token"},
	}
	out, err := frps.RenderConfig(cfg, "tok")
	require.NoError(t, err)
	require.Contains(t, out, "bindPort = 7000")
}

func TestRenderConfig_UnsupportedAuth(t *testing.T) {
	cfg := v1alpha1.FrpsConfig{
		Version:    "v0.68.1",
		BindPort:   7000,
		AllowPorts: []string{"80"},
		Auth:       v1alpha1.FrpsAuthConfig{Method: "oidc"},
	}
	_, err := frps.RenderConfig(cfg, "tok")
	require.Error(t, err)
}

func TestRenderConfig_OptionalListeners(t *testing.T) {
	httpPort := int32(80)
	httpsPort := int32(443)
	kcp := int32(7000)
	quic := int32(7001)
	cfg := v1alpha1.FrpsConfig{
		Version:        "v0.68.1",
		BindPort:       7000,
		VhostHTTPPort:  &httpPort,
		VhostHTTPSPort: &httpsPort,
		KCPBindPort:    &kcp,
		QUICBindPort:   &quic,
		AllowPorts:     []string{"80"},
		Auth:           v1alpha1.FrpsAuthConfig{Method: "token"},
	}
	out, err := frps.RenderConfig(cfg, "t")
	require.NoError(t, err)
	require.Contains(t, out, "vhostHTTPPort = 80")
	require.Contains(t, out, "vhostHTTPSPort = 443")
	require.Contains(t, out, "kcpBindPort = 7000")
	require.Contains(t, out, "quicBindPort = 7001")
}
