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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/test/utils"
	"github.com/mtaku3/frp-operator/test/utils/exitserver"
	"github.com/mtaku3/frp-operator/test/utils/kubernetes"
	"github.com/mtaku3/frp-operator/test/utils/tunnel"
)

var _ = Describe("ExitServer finalizer", Ordered, func() {
	const ns = "default"
	const tunnelName = "tunnel-basic"

	BeforeAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(kubernetes.ApplyServerSide(context.Background(), yaml)).To(Succeed())
		Expect(tunnel.WaitForPhase(context.Background(), k8sClient, ns, tunnelName,
			frpv1alpha1.TunnelReady, 4*time.Minute)).To(Succeed())
	})

	AfterAll(func() {
		yaml, err := os.ReadFile("fixtures/tunnel_basic.yaml")
		Expect(err).NotTo(HaveOccurred())
		_ = kubernetes.DeleteServerSide(context.Background(), yaml)
	})

	It("releases the docker container and credentials Secret on delete", func() {
		ctx := context.Background()
		exits, err := exitserver.List(ctx, k8sClient, ns)
		Expect(err).NotTo(HaveOccurred())
		Expect(exits).NotTo(BeEmpty())
		exit := exits[0]

		container := "frp-operator-default__" + exit.Name
		credSecret := exit.Name + "-credentials"

		out, err := utils.Run(exec.Command("docker", "inspect", "-f", "{{.Name}}", container))
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty())

		Expect(k8sClient.Delete(ctx, &exit)).To(Succeed())

		Eventually(func() error {
			_, e := utils.Run(exec.Command("docker", "inspect", container))
			return e
		}, 2*time.Minute, 2*time.Second).ShouldNot(Succeed())

		Eventually(func() bool {
			var s corev1.Secret
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: credSecret}, &s)
			return err != nil
		}, 2*time.Minute, 2*time.Second).Should(BeTrue())
	})
})
