// Package config renders FRP client and server TOML configuration. Pure
// data-in / bytes-out; no I/O. Controllers in later phases compose these
// outputs into Secrets, ConfigMaps, or cloud-init payloads.
package config

import (
	"bytes"

	"github.com/BurntSushi/toml"
)

// FrpcAuth carries the client-side authentication settings. Method is
// typically "token" with a corresponding shared Token.
type FrpcAuth struct {
	Method string `toml:"method,omitempty"`
	Token  string `toml:"token,omitempty"`
}

// FrpcProxy is one proxy entry written into the [[proxies]] array of frpc.toml.
// Name must be globally unique on the targeted frps; the operator namespaces
// it as "<tenant-ns>_<tunnel-name>_<port-name>".
type FrpcProxy struct {
	Name       string `toml:"name"`
	Type       string `toml:"type"` // "tcp" or "udp"
	LocalIP    string `toml:"localIP"`
	LocalPort  int    `toml:"localPort"`
	RemotePort int    `toml:"remotePort"`
}

// FrpcConfig is the in-memory representation of frpc.toml.
type FrpcConfig struct {
	ServerAddr string      `toml:"serverAddr"`
	ServerPort int         `toml:"serverPort"`
	Auth       FrpcAuth    `toml:"auth,omitempty"`
	Proxies    []FrpcProxy `toml:"proxies,omitempty"`
}

// Render encodes the config as TOML bytes suitable for /etc/frp/frpc.toml or
// for mounting into an frpc Pod via a Secret.
func (c FrpcConfig) Render() ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
