/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

// Package utils contains generic helpers shared by every e2e package.
// Run wraps os/exec with project-rooted CWD + GinkgoWriter logging.
package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck
)

const (
	defaultKindBinary  = "kind"
	defaultKindCluster = "frp-operator-test-e2e"
)

// Run executes cmd at the project root, capturing combined output and
// streaming the command line to GinkgoWriter so failures are debuggable.
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := getProjectDir()
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q (cwd=%s)\n", command, dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed: %w; output=%s", command, err, string(output))
	}
	return string(output), nil
}

// LoadImageToKindClusterWithName runs `kind load docker-image NAME --name CLUSTER`.
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	_, err := Run(exec.Command(kindBinary, "load", "docker-image", name, "--name", cluster))
	return err
}

// KindClusterName returns the configured kind cluster name (env or default).
func KindClusterName() string {
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		return v
	}
	return defaultKindCluster
}

// GetNonEmptyLines splits output on \n and drops empty entries.
func GetNonEmptyLines(output string) []string {
	var res []string
	for line := range strings.SplitSeq(output, "\n") {
		if line != "" {
			res = append(res, line)
		}
	}
	return res
}

func getProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("get cwd: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}
