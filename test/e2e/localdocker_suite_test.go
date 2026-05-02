//go:build e2e_localdocker
// +build e2e_localdocker

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package e2e (build tag e2e_localdocker) exercises the LocalDocker
// provisioner against a real kind cluster. Unlike the default e2e suite
// (which runs the operator in-cluster), this suite runs the manager
// out-of-cluster as a host subprocess so it can talk to the host Docker
// daemon directly. The operator is configured with
// LOCALDOCKER_NETWORK=kind so the frps containers it provisions are
// reachable from frpc Pods inside the kind cluster.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mtaku3/frp-operator/test/utils"
)

var (
	ldKindCluster   = envOr("KIND_CLUSTER", "frp-operator-test-e2e")
	ldKubeconfig    = filepath.Join(os.TempDir(), "frp-operator-localdocker.kubeconfig")
	ldManagerBinary = filepath.Join(os.TempDir(), "frp-operator-manager-e2e")
	ldOperatorCmd   *exec.Cmd
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envWithoutKubeconfig returns os.Environ() with any pre-set KUBECONFIG
// stripped, so the caller can append the suite's kubeconfig and have it
// win deterministically (glibc getenv returns the first match — the
// ambient KUBECONFIG from devbox would otherwise shadow ours).
func envWithoutKubeconfig() []string {
	in := os.Environ()
	out := make([]string, 0, len(in))
	for _, kv := range in {
		if strings.HasPrefix(kv, "KUBECONFIG=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// TestE2ELocalDocker runs the LocalDocker e2e suite.
func TestE2ELocalDocker(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting frp-operator LocalDocker e2e suite\n")
	RunSpecs(t, "e2e localdocker suite")
}

var _ = BeforeSuite(func() {
	By("removing any leftover frps containers from prior killed runs")
	// Best-effort; AfterSuite removes them after a clean run, but a
	// killed/aborted run can leave them behind and block new
	// Provisioner.Create with a name conflict.
	_, _ = utils.Run(exec.Command("bash", "-c",
		`docker ps -a --format '{{.Names}}' | grep '^frp-operator-default__' | xargs -r docker rm -f`))

	By("exporting kubeconfig for the kind cluster")
	cmd := exec.Command("kind", "export", "kubeconfig",
		"--name", ldKindCluster,
		"--kubeconfig", ldKubeconfig)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to export kind kubeconfig")

	By("installing CRDs into the kind cluster")
	// Cold-cluster apiserver/etcd is slow for the first ~30s; retry on
	// transient timeouts so the suite isn't gated on warm-up jitter.
	Eventually(func() error {
		installCmd := exec.Command("make", "install")
		installCmd.Env = append(envWithoutKubeconfig(), "KUBECONFIG="+ldKubeconfig)
		_, e := utils.Run(installCmd)
		return e
	}, 90*time.Second, 5*time.Second).Should(Succeed(), "Failed to install CRDs")

	By("building the manager binary for the host")
	buildCmd := exec.Command("go", "build", "-o", ldManagerBinary, "./cmd/manager")
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	_, err = utils.Run(buildCmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to build manager binary")

	By("starting the operator as a host subprocess")
	ldOperatorCmd = exec.Command(ldManagerBinary,
		"--leader-elect=false",
		"--metrics-bind-address=:0",
		"--health-probe-bind-address=:18081",
	)
	ldOperatorCmd.Env = append(envWithoutKubeconfig(),
		"KUBECONFIG="+ldKubeconfig,
		"LOCALDOCKER_NETWORK=kind",
	)
	ldOperatorCmd.Stdout = GinkgoWriter
	ldOperatorCmd.Stderr = GinkgoWriter
	// Put the child in its own process group so we can SIGTERM it cleanly.
	ldOperatorCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	Expect(ldOperatorCmd.Start()).To(Succeed())

	By("waiting for the operator readyz endpoint")
	Eventually(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:18081/readyz", nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}, 60*time.Second, 2*time.Second).Should(Succeed(), "operator readyz never became OK")
})

var _ = AfterSuite(func() {
	By("stopping the operator subprocess")
	if ldOperatorCmd != nil && ldOperatorCmd.Process != nil {
		_ = syscall.Kill(-ldOperatorCmd.Process.Pid, syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- ldOperatorCmd.Wait() }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = syscall.Kill(-ldOperatorCmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}

	By("cleaning up leftover frps containers")
	// Best-effort; don't fail the suite on cleanup hiccups.
	_, _ = utils.Run(exec.Command("bash", "-c",
		`docker ps -a --format '{{.Names}}' | grep '^frp-operator-default__' | xargs -r docker rm -f`))
})
