// Package bootstrap renders a cloud-init user-data script that installs
// frps on a freshly provisioned VPS and starts it under systemd. Pure
// rendering — no I/O.
package bootstrap

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

//go:embed cloudinit.tmpl
var cloudinitTmpl string

// Input is the data the cloud-init template needs.
type Input struct {
	// FrpsConfigTOML is the rendered frps.toml content. Base64-encoded
	// before being embedded in cloud-init's write_files content field.
	FrpsConfigTOML []byte

	BindPort  int
	AdminPort int

	// AllowPortsRange is a single contiguous tcp range string in the form
	// "<start>-<end>" — e.g., "1024-65535". UFW expects "<start>:<end>";
	// the renderer translates internally.
	AllowPortsRange string

	// ReservedPorts is a list of ports the operator must NOT expose. They
	// get explicit ufw deny rules.
	ReservedPorts []int

	FrpsVersion     string // e.g., "v0.65.0"
	FrpsDownloadURL string
	FrpsSHA256      string
}

// Render produces the cloud-init user-data bytes.
func Render(in Input) ([]byte, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	tmpl, err := template.New("cloudinit").Funcs(template.FuncMap{
		"trimv": func(s string) string { return strings.TrimPrefix(s, "v") },
	}).Parse(cloudinitTmpl)
	if err != nil {
		return nil, err
	}
	data := struct {
		Input
		FrpsConfigB64      string
		AllowPortsRangeUFW string
	}{
		Input:              in,
		FrpsConfigB64:      base64.StdEncoding.EncodeToString(in.FrpsConfigTOML),
		AllowPortsRangeUFW: strings.ReplaceAll(in.AllowPortsRange, "-", ":"),
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func validate(in Input) error {
	if len(in.FrpsConfigTOML) == 0 {
		return fmt.Errorf("FrpsConfigTOML is empty")
	}
	if in.BindPort == 0 || in.AdminPort == 0 {
		return fmt.Errorf("BindPort and AdminPort are required")
	}
	if err := validateAllowPortsRange(in.AllowPortsRange); err != nil {
		return err
	}
	if in.FrpsDownloadURL == "" || in.FrpsSHA256 == "" || in.FrpsVersion == "" {
		return fmt.Errorf("FrpsVersion, FrpsDownloadURL, FrpsSHA256 are all required")
	}
	if len(in.FrpsSHA256) != 64 {
		return fmt.Errorf("FrpsSHA256 must be 64 hex chars, got len %d", len(in.FrpsSHA256))
	}
	return nil
}

func validateAllowPortsRange(allowPortsRange string) error {
	re := regexp.MustCompile(`^\d+-\d+$`)
	if !re.MatchString(allowPortsRange) {
		return fmt.Errorf("AllowPortsRange must be of the form start-end (numeric), got %q", allowPortsRange)
	}

	parts := strings.Split(allowPortsRange, "-")
	start, _ := strconv.Atoi(parts[0])
	end, _ := strconv.Atoi(parts[1])

	if end < start {
		return fmt.Errorf("AllowPortsRange end (%d) must be >= start (%d)", end, start)
	}

	return nil
}
