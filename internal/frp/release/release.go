// Package release exposes the pinned upstream FRP release the operator ships
// to provisioned VPSes. The version, URLs, and checksums change in lockstep
// with the operator binary — they are not user-configurable at runtime.
package release

import (
	"fmt"
	"strings"
)

// Version is the upstream FRP release tag bundled with this operator build.
const Version = "v0.68.1"

// SHA256 checksums are taken from the upstream release page's
// frp_sha256_checksums.txt.
const (
	SHA256LinuxAmd64 = "4a4e88987d39561e1b3b3b23d0ede48a457eebf76a87231999957e870f5f02b6"
	SHA256LinuxArm64 = "e7ad15b0cfe4cf0125df4217778b66cb4426179270967b59900ecb2362d8cd01"
)

// DownloadURL returns the upstream tarball URL for the pinned Version.
func DownloadURL(goos, goarch string) string {
	return fmt.Sprintf(
		"https://github.com/fatedier/frp/releases/download/%s/frp_%s_%s_%s.tar.gz",
		Version, strings.TrimPrefix(Version, "v"), goos, goarch,
	)
}
