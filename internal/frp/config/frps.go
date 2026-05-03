package config

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

// FrpsAuth carries server-side auth settings. Mirrors FrpcAuth so a matched
// pair (same Method+Token) authenticates a client to a server.
type FrpsAuth struct {
	Method string `toml:"method,omitempty"`
	Token  string `toml:"token,omitempty"`
}

// FrpsWebServer configures frps's admin REST API endpoint. The operator
// connects to <Addr>:<Port> using HTTP basic auth (User/Password) to push
// config and reload.
type FrpsWebServer struct {
	Addr     string `toml:"addr"`
	Port     int    `toml:"port"`
	User     string `toml:"user,omitempty"`
	Password string `toml:"password,omitempty"`
}

// FrpsPortRange is either a single port (Single != 0) or a [Start, End]
// range. FRP's allowPorts TOML expects strings like "80" or "1024-65535";
// MarshalTOML below produces those.
type FrpsPortRange struct {
	Single int
	Start  int
	End    int
}

// MarshalTOML implements toml.Marshaler so FrpsPortRange serializes as the
// FRP-expected string form. (frp's config schema for allowPorts is an
// array of {single = 80} or {start = 1024, end = 65535} tables in modern
// versions; the operator uses the table form for clarity and round-trip.)
func (p FrpsPortRange) MarshalTOML() ([]byte, error) {
	if p.Single != 0 {
		return fmt.Appendf(nil, "{ single = %d }", p.Single), nil
	}
	return fmt.Appendf(nil, "{ start = %d, end = %d }", p.Start, p.End), nil
}

// FrpsConfig is the in-memory representation of frps.toml.
type FrpsConfig struct {
	BindPort   int             `toml:"bindPort"`
	Auth       FrpsAuth        `toml:"auth,omitempty"`
	WebServer  FrpsWebServer   `toml:"webServer,omitempty"`
	AllowPorts []FrpsPortRange `toml:"allowPorts,omitempty"`
}

// Render encodes the config as TOML bytes suitable for /etc/frp/frps.toml.
func (c FrpsConfig) Render() ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
