package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCloudInitBasic(t *testing.T) {
	in := Input{
		FrpsConfigTOML:  []byte("bindPort = 7000\n[auth]\ntoken = \"abc\"\n"),
		AdminPort:       7500,
		BindPort:        7000,
		AllowPortsRange: "1024-65535",
		ReservedPorts:   []int{22, 7000, 7500},
		FrpsVersion:     "v0.65.0",
		FrpsDownloadURL: "https://github.com/fatedier/frp/releases/download/v0.65.0/frp_0.65.0_linux_amd64.tar.gz",
		FrpsSHA256:      "abc1234567890123456789012345678901234567890123456789012345678901",
	}
	got, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "cloudinit_basic.yaml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderCloudInitContainsExpectedKeys(t *testing.T) {
	in := Input{
		FrpsConfigTOML:  []byte("bindPort = 7000\n"),
		AdminPort:       7500,
		BindPort:        7000,
		AllowPortsRange: "1024-65535",
		ReservedPorts:   []int{22, 7000, 7500},
		FrpsVersion:     "v0.65.0",
		FrpsDownloadURL: "https://example.test/frp.tar.gz",
		FrpsSHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
	}
	got, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"#cloud-config",
		"runcmd:",
		"https://example.test/frp.tar.gz",
		"sha256sum",
		"frps.service",
		"ufw allow 7000",
		"ufw allow 7500",
		"ufw allow 1024:65535",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered cloud-init missing %q", want)
		}
	}
}
