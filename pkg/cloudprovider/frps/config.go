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
	bindAddr := "0.0.0.0"
	bindPort := cfg.BindPort
	if bindPort == 0 {
		bindPort = 7000
	}
	fmt.Fprintf(&b, "bindAddr = %q\n", bindAddr)
	fmt.Fprintf(&b, "bindPort = %d\n", bindPort)
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
	if err := writeAllowPorts(&b, cfg.AllowPorts); err != nil {
		return "", err
	}
	if cfg.AdminPort != 0 {
		// Section form mandatory for nested keys per frp v0.61+ TOML schema;
		// flat-dotted notation parses but silently drops the values in some
		// builds.
		b.WriteString("\n[webServer]\n")
		fmt.Fprintf(&b, "addr = %q\n", "0.0.0.0")
		fmt.Fprintf(&b, "port = %d\n", cfg.AdminPort)
	}
	switch cfg.Auth.Method {
	case "token", "":
		b.WriteString("\n[auth]\n")
		b.WriteString("method = \"token\"\n")
		fmt.Fprintf(&b, "token = %q\n", authToken)
	default:
		return "", fmt.Errorf("unsupported auth method %q", cfg.Auth.Method)
	}
	if cfg.TLS != nil && cfg.TLS.Force {
		b.WriteString("\n[transport.tls]\n")
		b.WriteString("force = true\n")
	}
	return b.String(), nil
}

// writeAllowPorts emits TOML for the allowPorts inline-table-array shape
// frps expects: [{single = 80}, {start = 1024, end = 65535}, ...].
// Input strings are either "<n>" or "<lo>-<hi>".
func writeAllowPorts(b *strings.Builder, allow []string) error {
	if len(allow) == 0 {
		return nil
	}
	b.WriteString("allowPorts = [\n")
	for _, p := range allow {
		if i := strings.IndexByte(p, '-'); i > 0 {
			lo := strings.TrimSpace(p[:i])
			hi := strings.TrimSpace(p[i+1:])
			fmt.Fprintf(b, "  { start = %s, end = %s },\n", lo, hi)
			continue
		}
		fmt.Fprintf(b, "  { single = %s },\n", strings.TrimSpace(p))
	}
	b.WriteString("]\n")
	return nil
}
