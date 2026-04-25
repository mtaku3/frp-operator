package release

import (
	"strings"
	"testing"
)

func TestDownloadURLLinuxAmd64(t *testing.T) {
	url := DownloadURL("linux", "amd64")
	if !strings.HasPrefix(url, "https://github.com/fatedier/frp/releases/download/") {
		t.Errorf("unexpected host/path: %s", url)
	}
	if !strings.Contains(url, Version) {
		t.Errorf("URL missing version %q: %s", Version, url)
	}
	if !strings.Contains(url, "linux_amd64") {
		t.Errorf("URL missing platform: %s", url)
	}
	if !strings.HasSuffix(url, ".tar.gz") {
		t.Errorf("URL missing extension: %s", url)
	}
}

func TestVersionAndChecksumPopulated(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
	if !strings.HasPrefix(Version, "v") {
		t.Errorf("Version must start with v, got %q", Version)
	}
	if SHA256LinuxAmd64 == "" || len(SHA256LinuxAmd64) != 64 {
		t.Errorf("SHA256LinuxAmd64 must be a 64-char hex string, got %q (len=%d)",
			SHA256LinuxAmd64, len(SHA256LinuxAmd64))
	}
}
