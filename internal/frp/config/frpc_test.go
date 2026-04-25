package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderFrpcMinimal(t *testing.T) {
	cfg := FrpcConfig{
		ServerAddr: "203.0.113.10",
		ServerPort: 7000,
		Auth:       FrpcAuth{Method: "token", Token: "secret-token-1234"},
		Proxies: []FrpcProxy{
			{
				Name:       "my-ns_my-tunnel_http",
				Type:       "tcp",
				LocalIP:    "10.0.0.42",
				LocalPort:  80,
				RemotePort: 80,
			},
		},
	}
	got, err := cfg.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := readGolden(t, "frpc_minimal.toml")
	if normalize(string(got)) != normalize(want) {
		t.Errorf("frpc_minimal output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderFrpcMultiProxy(t *testing.T) {
	cfg := FrpcConfig{
		ServerAddr: "203.0.113.20",
		ServerPort: 7000,
		Auth:       FrpcAuth{Method: "token", Token: "tok"},
		Proxies: []FrpcProxy{
			{Name: "ns_t1_http", Type: "tcp", LocalIP: "10.0.0.1", LocalPort: 80, RemotePort: 80},
			{Name: "ns_t1_https", Type: "tcp", LocalIP: "10.0.0.1", LocalPort: 443, RemotePort: 443},
			{Name: "ns_t2_pg", Type: "tcp", LocalIP: "10.0.0.2", LocalPort: 5432, RemotePort: 5432},
		},
	}
	got, err := cfg.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := readGolden(t, "frpc_multi_proxy.toml")
	if normalize(string(got)) != normalize(want) {
		t.Errorf("frpc_multi_proxy output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	return string(b)
}

// normalize removes trailing whitespace per line and trims leading/trailing
// blank lines so golden comparisons are tolerant of TOML serializer quirks.
func normalize(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n") + "\n"
}
