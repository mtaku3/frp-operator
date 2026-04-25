package bootstrap_test

import (
	"strings"
	"testing"

	"github.com/mtaku3/frp-operator/internal/bootstrap"
	"github.com/mtaku3/frp-operator/internal/frp/config"
	"github.com/mtaku3/frp-operator/internal/frp/release"
)

func TestComposedRender(t *testing.T) {
	cfg := config.FrpsConfig{
		BindPort: 7000,
		Auth:     config.FrpsAuth{Method: "token", Token: "compose-test-token"},
		WebServer: config.FrpsWebServer{
			Addr: "0.0.0.0", Port: 7500, User: "admin", Password: "compose-pw",
		},
		AllowPorts: []config.FrpsPortRange{{Start: 1024, End: 65535}},
	}
	body, err := cfg.Render()
	if err != nil {
		t.Fatalf("frps render: %v", err)
	}

	out, err := bootstrap.Render(bootstrap.Input{
		FrpsConfigTOML:  body,
		BindPort:        7000,
		AdminPort:       7500,
		AllowPortsRange: "1024-65535",
		ReservedPorts:   []int{22, 7000, 7500},
		FrpsVersion:     release.Version,
		FrpsDownloadURL: release.DownloadURL("linux", "amd64"),
		FrpsSHA256:      release.SHA256LinuxAmd64,
	})
	if err != nil {
		t.Fatalf("bootstrap render: %v", err)
	}

	s := string(out)
	for _, want := range []string{
		"#cloud-config",
		release.Version,
		release.SHA256LinuxAmd64,
		"frps.service",
		"systemctl enable --now frps.service",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("composed render missing %q", want)
		}
	}
}
