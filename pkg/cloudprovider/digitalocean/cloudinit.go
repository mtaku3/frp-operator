package digitalocean

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps"
)

// defaultBinaryURLTemplate is the GitHub release URL pattern for the frps
// archive. Two %s slots: version (twice).
//
// NOTE: pc.Spec.DefaultImage carries this template — but per Phase 1
// review the field default is currently the misnamed container reference
// "fatedier/frps:%s". When that default is set, we override it with this
// proper binary URL template. The CRD default fix is deferred to Phase 9
// (requires regenerating manifests).
const defaultBinaryURLTemplate = "https://github.com/fatedier/frp/releases/download/%s/frp_%s_linux_amd64.tar.gz"

// binaryURL returns the frps archive URL for a given template + version.
// If template doesn't look like a URL (e.g. the historic
// "fatedier/frps:%s" container reference), the binary URL template is
// substituted instead.
func binaryURL(template, version string) string {
	tpl := template
	if !strings.HasPrefix(tpl, "http://") && !strings.HasPrefix(tpl, "https://") {
		tpl = defaultBinaryURLTemplate
	}
	// Trim leading "v" off the version for the second slot in the URL
	// (the GitHub release path uses /vX.Y.Z/ but the tarball is
	// frp_X.Y.Z_linux_amd64.tar.gz).
	verNoV := strings.TrimPrefix(version, "v")
	return fmt.Sprintf(tpl, version, verNoV)
}

// RenderCloudInit produces a cloud-init script that:
// 1. Downloads frps from the GitHub release URL.
// 2. Extracts and installs the binary to /usr/local/bin/frps.
// 3. Writes /etc/frp/frps.toml from frps.RenderConfig output.
// 4. Drops a systemd unit and starts it.
func RenderCloudInit(frpsCfg v1alpha1.FrpsConfig, authToken, binaryURLTemplate string) (string, error) {
	body, err := frps.RenderConfig(frpsCfg, authToken)
	if err != nil {
		return "", err
	}
	url := binaryURL(binaryURLTemplate, frpsCfg.Version)
	verNoV := strings.TrimPrefix(frpsCfg.Version, "v")

	var b strings.Builder
	b.WriteString("#cloud-config\n")
	b.WriteString("write_files:\n")
	b.WriteString("  - path: /etc/frp/frps.toml\n")
	b.WriteString("    permissions: '0600'\n")
	b.WriteString("    owner: root:root\n")
	b.WriteString("    content: |\n")
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintf(&b, "      %s\n", line)
	}
	b.WriteString("  - path: /etc/systemd/system/frps.service\n")
	b.WriteString("    permissions: '0644'\n")
	b.WriteString("    owner: root:root\n")
	b.WriteString("    content: |\n")
	b.WriteString("      [Unit]\n")
	b.WriteString("      Description=frps\n")
	b.WriteString("      After=network.target\n")
	b.WriteString("      [Service]\n")
	b.WriteString("      Type=simple\n")
	b.WriteString("      ExecStart=/usr/local/bin/frps -c /etc/frp/frps.toml\n")
	b.WriteString("      Restart=on-failure\n")
	b.WriteString("      [Install]\n")
	b.WriteString("      WantedBy=multi-user.target\n")
	b.WriteString("runcmd:\n")
	fmt.Fprintf(&b, "  - mkdir -p /opt/frp\n")
	fmt.Fprintf(&b, "  - curl -fsSL %q -o /tmp/frp.tar.gz\n", url)
	fmt.Fprintf(&b, "  - tar -xzf /tmp/frp.tar.gz -C /opt/frp --strip-components=1\n")
	fmt.Fprintf(&b, "  - install -m 0755 /opt/frp/frps /usr/local/bin/frps\n")
	_ = verNoV
	b.WriteString("  - systemctl daemon-reload\n")
	b.WriteString("  - systemctl enable --now frps.service\n")
	return b.String(), nil
}
