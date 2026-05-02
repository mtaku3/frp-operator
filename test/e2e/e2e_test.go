//go:build e2e
// +build e2e

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mtaku3/frp-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "frp-operator-system"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("undeploying the controller-manager")
		cmd := exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	It("the manager Pod is Running", func() {
		By("waiting for the controller-manager Pod")
		Eventually(func() error {
			cmd := exec.Command("kubectl", "wait",
				"--for=condition=Ready", "pod",
				"-l", "control-plane=controller-manager",
				"-n", namespace, "--timeout=120s")
			_, err := utils.Run(cmd)
			return err
		}, 3*time.Minute, 5*time.Second).Should(Succeed())

		By("recording the controller pod name for failure diagnostics")
		cmd := exec.Command("kubectl", "get",
			"pods", "-l", "control-plane=controller-manager",
			"-o", "go-template={{ range .items }}"+
				"{{ if not .metadata.deletionTimestamp }}"+
				"{{ .metadata.name }}"+
				"{{ \"\\n\" }}{{ end }}{{ end }}",
			"-n", namespace,
		)
		podOutput, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
		podNames := utils.GetNonEmptyLines(podOutput)
		Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
		controllerPodName = podNames[0]
	})

	It("reconciles a Tunnel CR through Allocating", func() {
		By("creating a SchedulingPolicy")
		sp := []byte(`
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata:
  name: default
spec:
  consolidation:
    reclaimEmpty: false
  vps:
    default:
      provider: digitalocean
      regions: ["nyc1"]
      size: s-1vcpu-1gb
`)
		Expect(applyManifest(sp)).To(Succeed())

		By("creating a Tunnel CR with no exit available")
		tunnel := []byte(`
apiVersion: frp.operator.io/v1alpha1
kind: Tunnel
metadata:
  name: e2e-tunnel
  namespace: default
spec:
  service:
    name: my-svc
    namespace: default
  ports:
    - name: http
      servicePort: 80
  schedulingPolicyRef:
    name: default
`)
		Expect(applyManifest(tunnel)).To(Succeed())

		By("waiting for the operator to reconcile to Allocating or Provisioning")
		Eventually(func() string {
			out, _ := utils.Run(exec.Command("kubectl", "get", "tunnel", "e2e-tunnel",
				"-n", "default", "-o", "jsonpath={.status.phase}"))
			return string(out)
		}, 60*time.Second, 2*time.Second).Should(Or(
			Equal("Allocating"),
			Equal("Provisioning"),
		))

		By("cleaning up")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "tunnel", "e2e-tunnel", "-n", "default", "--wait=false"))
		_, _ = utils.Run(exec.Command("kubectl", "delete", "schedulingpolicy", "default", "--wait=false"))
	})

	It("ServiceWatcher creates a Tunnel from a matching Service", func() {
		By("creating a Service with the matching loadBalancerClass")
		svc := []byte(`
apiVersion: v1
kind: Service
metadata:
  name: e2e-svc
  namespace: default
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports:
    - name: http
      port: 80
      targetPort: 8080
      protocol: TCP
  selector:
    app: nonexistent
`)
		Expect(applyManifest(svc)).To(Succeed())

		By("expecting a sibling Tunnel CR to be created")
		Eventually(func() error {
			_, err := utils.Run(exec.Command("kubectl", "get", "tunnel", "e2e-svc", "-n", "default"))
			return err
		}, 60*time.Second, 2*time.Second).Should(Succeed())

		By("cleaning up")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "svc", "e2e-svc", "-n", "default", "--wait=false"))
		// Owner ref should cascade-delete the Tunnel; if not, force.
		_, _ = utils.Run(exec.Command("kubectl", "delete", "tunnel", "e2e-svc", "-n", "default", "--wait=false", "--ignore-not-found"))
	})

	// Webhook validation lives inside the Manager Describe so it
	// shares the in-cluster operator deployment created by the outer
	// BeforeAll. Outer AfterAll undeploys the operator, so a separate
	// Describe would run against an empty cluster.

	It("rejects spec change to a Ready+ImmutableWhenReady Tunnel", func() {
		if os.Getenv("E2E_WEBHOOK") != "1" {
			Skip("set E2E_WEBHOOK=1 to run webhook specs (requires cert-manager)")
		}

		By("creating a Tunnel with ImmutableWhenReady=true")
		Expect(applyManifest([]byte(`
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
		patch := `{"status":{"phase":"Ready"}}`
		_, err := utils.Run(exec.Command("kubectl", "patch", "tunnel", "wh-immutable",
			"-n", "default", "--type=merge", "--subresource=status", "-p", patch))
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

		By("cleaning up")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "tunnel", "wh-immutable",
			"-n", "default", "--ignore-not-found", "--wait=false"))
	})

	It("rejects shrinking ExitServer AllowPorts below allocations", func() {
		if os.Getenv("E2E_WEBHOOK") != "1" {
			Skip("set E2E_WEBHOOK=1 to run webhook specs (requires cert-manager)")
		}

		By("creating an ExitServer with a wide AllowPorts and frps spec")
		Expect(applyManifest([]byte(`
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
			"-n", "default", "--type=merge", "--subresource=status",
			"-p", `{"status":{"allocations":{"5000":"default/test"}}}`))
		Expect(err).NotTo(HaveOccurred())

		By("attempting to shrink AllowPorts to a range that drops port 5000")
		_, err = utils.Run(exec.Command("kubectl", "patch", "exitserver", "wh-grow",
			"-n", "default", "--type=merge",
			"-p", `{"spec":{"allowPorts":["1024-4999"]}}`))
		Expect(err).To(HaveOccurred(), "expected admission rejection")

		By("cleaning up")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "exitserver", "wh-grow",
			"-n", "default", "--ignore-not-found", "--wait=false"))
	})
})

// applyManifest writes the YAML to a temp file and runs `kubectl apply -f`.
func applyManifest(yaml []byte) error {
	f, err := os.CreateTemp("", "e2e-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(yaml); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_, err = utils.Run(exec.Command("kubectl", "apply", "-f", f.Name()))
	return err
}

