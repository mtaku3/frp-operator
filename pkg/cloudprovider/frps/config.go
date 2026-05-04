// Package frps renders frps daemon configuration (TOML) from the
// declarative v1alpha1.FrpsConfig.
package frps

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// RenderConfig produces the frps.toml body for a FrpsConfig + auth token.
// The token is the resolved value (caller fetched from Secret).
func RenderConfig(cfg v1alpha1.FrpsConfig, authToken string) (string, error) {
	var b strings.Builder
	bindPort := cfg.BindPort
	if bindPort == 0 {
		bindPort = 7000
	}
	fmt.Fprintf(&b, "bindPort = %d\n", bindPort)
	if cfg.AdminPort != 0 {
		b.WriteString("webServer.addr = \"0.0.0.0\"\n")
		fmt.Fprintf(&b, "webServer.port = %d\n", cfg.AdminPort)
	}
	if cfg.VhostHTTPPort != nil {
		fmt.Fprintf(&b, "vhostHTTPPort = %d\n", *cfg.VhostHTTPPort)
	}
	if cfg.VhostHTTPSPort != nil {
		fmt.Fprintf(&b, "vhostHTTPSPort = %d\n", *cfg.VhostHTTPSPort)
	}
	if cfg.KCPBindPort != nil {
		fmt.Fprintf(&b, "kcpBindPort = %d\n", *cfg.KCPBindPort)
	}
	if cfg.QUICBindPort != nil {
		fmt.Fprintf(&b, "quicBindPort = %d\n", *cfg.QUICBindPort)
	}
	if len(cfg.AllowPorts) > 0 {
		b.WriteString("allowPorts = [")
		for i, p := range cfg.AllowPorts {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", p)
		}
		b.WriteString("]\n")
	}
	switch cfg.Auth.Method {
	case "token", "":
		b.WriteString("auth.method = \"token\"\n")
		fmt.Fprintf(&b, "auth.token = %q\n", authToken)
	default:
		return "", fmt.Errorf("unsupported auth method %q", cfg.Auth.Method)
	}
	if cfg.TLS != nil {
		if cfg.TLS.Force {
			b.WriteString("transport.tls.force = true\n")
		}
		// Real cert/key/ca pathing is wired by lifecycle controller via
		// volumes; here we only render flags it must honor.
	}
	return b.String(), nil
}
