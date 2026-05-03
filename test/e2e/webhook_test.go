//go:build e2e
// +build e2e

/*
Copyright (C) 2026.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/agpl-3.0.html>.
*/

package e2e

import (
	"context"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
)

var _ = Describe("Webhook validation", Ordered, func() {
	const ns = "default"

	AfterAll(func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "tunnel", "wh-immutable",
			"-n", ns, "--ignore-not-found", "--wait=false"))
		_, _ = utils.Run(exec.Command("kubectl", "delete", "exitserver", "wh-grow",
			"-n", ns, "--ignore-not-found", "--wait=false"))
	})

	It("rejects spec change to a Ready+ImmutableWhenReady Tunnel", func() {
		ctx := context.Background()

		By("creating a Tunnel with ImmutableWhenReady=true")
		Expect(kubernetes.ApplyServerSide(ctx, []byte(`
apiVersion: frp.operator.io/v1alpha1
kind: Tunnel
metadata: {name: wh-immutable, namespace: default}
spec:
  immutableWhenReady: true
  service: {name: wh-svc, namespace: default}
  ports: [{name: http, servicePort: 80}]
  schedulingPolicyRef: {name: default}
`))).To(Succeed())

		By("forcing status.phase=Ready via the status subresource")
		_, err := utils.Run(exec.Command("kubectl", "patch", "tunnel", "wh-immutable",
			"-n", ns, "--type=merge", "--subresource=status",
			"-p", `{"status":{"phase":"Ready"}}`))
		Expect(err).NotTo(HaveOccurred())

		By("attempting to mutate the locked spec.service.name; expect rejection")
		mutated := []byte(`
apiVersion: frp.operator.io/v1alpha1
kind: Tunnel
metadata: {name: wh-immutable, namespace: default}
spec:
  immutableWhenReady: true
  service: {name: wh-svc-MUTATED, namespace: default}
  ports: [{name: http, servicePort: 80}]
  schedulingPolicyRef: {name: default}
`)
		f, ferr := os.CreateTemp("", "wh-*.yaml")
		Expect(ferr).NotTo(HaveOccurred())
		defer os.Remove(f.Name())
		_, _ = f.Write(mutated)
		_ = f.Close()

		out, err := utils.Run(exec.Command("kubectl", "apply", "-f", f.Name()))
		Expect(err).To(HaveOccurred(), "expected admission rejection, got success: %s", out)
		Expect(out + err.Error()).To(ContainSubstring("immutable"))
	})

	It("rejects shrinking ExitServer AllowPorts below allocations", func() {
		ctx := context.Background()

		By("creating an ExitServer with a wide AllowPorts and frps spec")
		Expect(kubernetes.ApplyServerSide(ctx, []byte(`
apiVersion: frp.operator.io/v1alpha1
kind: ExitServer
metadata: {name: wh-grow, namespace: default}
spec:
  provider: local-docker
  frps: {version: v0.68.1, bindPort: 7000, adminPort: 7500}
  ssh: {port: 22}
  credentialsRef: {name: local-docker-credentials, key: token}
  allowPorts: ["1024-65535"]
`))).To(Succeed())

		By("seeding status.allocations[5000] via the status subresource")
		_, err := utils.Run(exec.Command("kubectl", "patch", "exitserver", "wh-grow",
			"-n", ns, "--type=merge", "--subresource=status",
			"-p", `{"status":{"allocations":{"5000":"default/test"}}}`))
		Expect(err).NotTo(HaveOccurred())

		By("attempting to shrink AllowPorts to a range that drops port 5000")
		_, err = utils.Run(exec.Command("kubectl", "patch", "exitserver", "wh-grow",
			"-n", ns, "--type=merge",
			"-p", `{"spec":{"allowPorts":["1024-4999"]}}`))
		Expect(err).To(HaveOccurred(), "expected admission rejection")
	})
})
