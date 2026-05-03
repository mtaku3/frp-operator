//go:build e2e_localdocker
// +build e2e_localdocker

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/mtaku3/frp-operator/test/utils"
)

const (
	ldNamespace      = "default"
	ldServiceName    = "ld-svc"
	ldBackendBody    = "hello-from-frp-e2e"
	ldKindNodeFmt    = "%s-control-plane" // kind names control-plane container as <cluster>-control-plane
)

var _ = Describe("LocalDocker provider integration", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	BeforeAll(func() {
		By("applying SchedulingPolicy with provider=local-docker and AllowPorts covering 80")
		// Name MUST be "default": ExitReclaimReconciler.PolicyName defaults
		// to "default" (no env wiring in the operator binary), so a
		// differently-named policy means reclaim falls back to its
		// hardcoded enabled=true and starts flapping the freshly-created
		// exit between Ready and Draining, hammering apiserver.
		Expect(applyManifestKC([]byte(`
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata:
  name: default
spec:
  consolidation:
    reclaimEmpty: false
  vps:
    default:
      provider: local-docker
      allowPorts: ["80", "1024-65535"]
`))).To(Succeed())

		By("applying credentials Secret expected by the provisioner")
		Expect(applyManifestKC([]byte(`
apiVersion: v1
kind: Secret
metadata:
  name: local-docker-credentials
  namespace: default
type: Opaque
stringData:
  token: e2e-localdocker-unused
`))).To(Succeed())

		By("applying backend Deployment")
		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ld-backend
  namespace: %s
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ld-backend
  template:
    metadata:
      labels:
        app: ld-backend
    spec:
      containers:
        - name: http-echo
          image: hashicorp/http-echo
          args: ["-text=%s", "-listen=:8080"]
          ports: [{containerPort: 8080}]
`, ldNamespace, ldBackendBody)))).To(Succeed())

		By("applying Service of type=LoadBalancer with the operator's class")
		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
  annotations:
    frp-operator.io/scheduling-policy: default
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports:
    - name: http
      port: 80
      targetPort: 8080
      protocol: TCP
  selector:
    app: ld-backend
`, ldServiceName, ldNamespace)))).To(Succeed())
	})

	AfterAll(func() {
		_, _ = runKC("delete", "svc", ldServiceName, "-n", ldNamespace, "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "deploy", "ld-backend", "-n", ldNamespace, "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "tunnel", "--all", "-n", ldNamespace, "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "exitserver", "--all", "-n", ldNamespace, "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", ldNamespace, "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
	})

	It("ServiceWatcher creates a Tunnel and the operator drives it to Ready", func() {
		By("waiting for ServiceWatcher to create a sibling Tunnel")
		Eventually(func() error {
			_, err := runKC("get", "tunnel", ldServiceName, "-n", ldNamespace)
			return err
		}).Should(Succeed())

		By("waiting for ExitServer to reach phase=Ready with a kind-net IP")
		Eventually(func() string {
			out, _ := runKC("get", "tunnel", ldServiceName, "-n", ldNamespace,
				"-o", "jsonpath={.status.assignedExit}")
			return out
		}).ShouldNot(BeEmpty())

		Eventually(func() string {
			exitName, _ := runKC("get", "tunnel", ldServiceName, "-n", ldNamespace,
				"-o", "jsonpath={.status.assignedExit}")
			if exitName == "" {
				return ""
			}
			out, _ := runKC("get", "exitserver", exitName, "-n", ldNamespace,
				"-o", "jsonpath={.status.phase}")
			return out
		}).Should(Equal("Ready"))

		By("waiting for the Tunnel to reach phase=Ready")
		Eventually(func() string {
			out, _ := runKC("get", "tunnel", ldServiceName, "-n", ldNamespace,
				"-o", "jsonpath={.status.phase}")
			return out
		}).Should(Equal("Ready"))
	})

	It("traffic flows from a kind node through frps to the backend", func() {
		By("resolving the Service ingress IP")
		var ingressIP string
		Eventually(func() string {
			out, _ := runKC("get", "svc", ldServiceName, "-n", ldNamespace,
				"-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
			ingressIP = strings.TrimSpace(out)
			return ingressIP
		}).ShouldNot(BeEmpty())

		By("curl-ing the ingress from inside a kind node")
		node := fmt.Sprintf(ldKindNodeFmt, ldKindCluster)
		Eventually(func() string {
			out, err := utils.Run(exec.Command("docker", "exec", node,
				"curl", "-s", "--max-time", "5", "http://"+ingressIP+":80"))
			if err != nil {
				return ""
			}
			return strings.TrimSpace(out)
		}).Should(Equal(ldBackendBody))
	})
})

// runKC invokes kubectl with the suite's KUBECONFIG.
func runKC(args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+ldKubeconfig)
	return utils.Run(cmd)
}

// applyManifestKC writes yaml to a temp file and `kubectl apply`s it
// against the suite's kubeconfig.
func applyManifestKC(yaml []byte) error {
	f, err := os.CreateTemp("", "ld-e2e-*.yaml")
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
	_, err = runKC("apply", "-f", f.Name())
	return err
}

var _ = Describe("Lifecycle", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	const lcSvc = "lc-svc"
	const lcBackend = "lc-backend"

	BeforeAll(func() {
		// The previous Describe block tears down with --wait=false; wait
		// for any lingering Tunnels and ExitServers to be fully gone
		// before we set up, otherwise the scheduler may pack our Tunnel
		// onto an ExitServer that's about to be deleted.
		Eventually(func() string {
			out, _ := runKC("get", "tunnel,exitserver", "-n", "default",
				"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
			return strings.TrimSpace(out)
		}, 60*time.Second, 2*time.Second).Should(BeEmpty())

		Expect(applyManifestKC([]byte(`
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata:
  name: default
spec:
  consolidation:
    reclaimEmpty: false
  vps:
    default:
      provider: local-docker
      allowPorts: ["80", "1024-65535"]
`))).To(Succeed())

		Expect(applyManifestKC([]byte(`
apiVersion: v1
kind: Secret
metadata:
  name: local-docker-credentials
  namespace: default
type: Opaque
stringData:
  token: lifecycle-unused
`))).To(Succeed())

		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: default
spec:
  replicas: 1
  selector: {matchLabels: {app: %s}}
  template:
    metadata: {labels: {app: %s}}
    spec:
      containers:
        - name: http-echo
          image: hashicorp/http-echo
          args: ["-text=lc", "-listen=:8080"]
          ports: [{containerPort: 8080}]
`, lcBackend, lcBackend, lcBackend)))).To(Succeed())

		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: default
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 80, targetPort: 8080, protocol: TCP}]
  selector: {app: %s}
`, lcSvc, lcBackend)))).To(Succeed())

		By("waiting for the Tunnel to reach Ready before lifecycle assertions")
		Eventually(func() string {
			out, _ := runKC("get", "tunnel", lcSvc, "-n", "default",
				"-o", "jsonpath={.status.phase}")
			return out
		}).Should(Equal("Ready"))
	})

	AfterAll(func() {
		_, _ = runKC("delete", "svc", lcSvc, "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "deploy", lcBackend, "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", "default", "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
		// Belt-and-suspenders: ensure no Tunnels/ExitServers leak into the
		// next Describe block.
		Eventually(func() string {
			out, _ := runKC("get", "tunnel,exitserver", "-n", "default",
				"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
			return strings.TrimSpace(out)
		}, 60*time.Second, 2*time.Second).Should(BeEmpty())
	})

	It("Tunnel deletion releases the port allocation on the exit", func() {
		By("recording the assigned exit and confirming port 80 is allocated")
		exit, err := runKC("get", "tunnel", lcSvc, "-n", "default",
			"-o", "jsonpath={.status.assignedExit}")
		Expect(err).NotTo(HaveOccurred())
		Expect(exit).NotTo(BeEmpty())

		alloc, err := runKC("get", "exitserver", exit, "-n", "default",
			"-o", `jsonpath={.status.allocations.80}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(alloc).NotTo(BeEmpty(), "expected port 80 allocation before delete")

		By("deleting the owning Service so the Tunnel cascades away")
		// Deleting the Tunnel directly while the Service still exists
		// causes ServiceWatcher to recreate it instantly with the same
		// name, so the port allocation never appears released. Removing
		// the Service first lets the owner-ref cascade-delete the Tunnel.
		_, err = runKC("delete", "svc", lcSvc, "-n", "default", "--wait=true")
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() string {
			out, _ := runKC("get", "tunnel", lcSvc, "-n", "default",
				"--ignore-not-found", "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(BeEmpty())

		By("waiting for port 80 to drop from the exit's allocations")
		Eventually(func() string {
			out, _ := runKC("get", "exitserver", exit, "-n", "default",
				"-o", `jsonpath={.status.allocations.80}`)
			return out
		}).Should(BeEmpty())

		By("waiting for the frpc Deployment to be garbage-collected")
		Eventually(func() string {
			out, _ := runKC("get", "deploy", lcSvc+"-frpc", "-n", "default",
				"--ignore-not-found", "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(BeEmpty())
	})

	It("ExitServer deletion runs the finalizer (container + cred secret gone)", func() {
		By("listing the empty exit left over from the previous spec")
		out, err := runKC("get", "exitserver", "-n", "default",
			"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
		Expect(err).NotTo(HaveOccurred())
		names := strings.Fields(out)
		Expect(names).NotTo(BeEmpty(), "expected at least one ExitServer")
		exit := names[0]

		container := "frp-operator-default__" + exit
		credSecret := exit + "-credentials"

		By("verifying the docker container exists before delete")
		dockerOut, err := utils.Run(exec.Command("docker", "inspect", "-f", "{{.Name}}", container))
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(dockerOut)).NotTo(BeEmpty())

		By("deleting the ExitServer")
		_, err = runKC("delete", "exitserver", exit, "-n", "default", "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the container to be removed by the finalizer")
		Eventually(func() error {
			_, e := utils.Run(exec.Command("docker", "inspect", container))
			return e
		}).ShouldNot(Succeed())

		By("waiting for the operator-managed credentials Secret to be gone")
		Eventually(func() string {
			s, _ := runKC("get", "secret", credSecret, "-n", "default",
				"--ignore-not-found", "-o", "jsonpath={.metadata.name}")
			return s
		}).Should(BeEmpty())
	})
})

var _ = Describe("Scheduling", Ordered, func() {
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	const svcA = "sched-a"
	const svcB = "sched-b"

	BeforeAll(func() {
		Eventually(func() string {
			out, _ := runKC("get", "tunnel,exitserver", "-n", "default",
				"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
			return strings.TrimSpace(out)
		}, 60*time.Second, 2*time.Second).Should(BeEmpty())

		Expect(applyManifestKC([]byte(`
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata:
  name: default
spec:
  consolidation:
    reclaimEmpty: false
  vps:
    default:
      provider: local-docker
      allowPorts: ["80", "81", "1024-65535"]
`))).To(Succeed())

		Expect(applyManifestKC([]byte(`
apiVersion: v1
kind: Secret
metadata: {name: local-docker-credentials, namespace: default}
type: Opaque
stringData: {token: sched-unused}
`))).To(Succeed())

		for _, n := range []struct{ name, label, port string }{
			{"sched-a-be", "sched-a", "8080"},
			{"sched-b-be", "sched-b", "8081"},
		} {
			Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: default
spec:
  replicas: 1
  selector: {matchLabels: {app: %s}}
  template:
    metadata: {labels: {app: %s}}
    spec:
      containers:
        - name: http-echo
          image: hashicorp/http-echo
          args: ["-text=%s", "-listen=:%s"]
          ports: [{containerPort: %s}]
`, n.name, n.label, n.label, n.label, n.port, n.port)))).To(Succeed())
		}

		// Apply A first and wait for its exit to be Ready before applying
		// B, otherwise both Tunnels race the scheduler before any exit
		// finishes provisioning and we end up with one exit per tunnel
		// instead of binpacked onto one.
		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata: {name: %s, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 80, targetPort: 8080, protocol: TCP}]
  selector: {app: sched-a}
`, svcA)))).To(Succeed())

		Eventually(func() string {
			out, _ := runKC("get", "tunnel", svcA, "-n", "default",
				"-o", "jsonpath={.status.phase}")
			return out
		}, 3*time.Minute, 2*time.Second).Should(Equal("Ready"))

		// Give the operator's informer cache a moment to observe the
		// ExitServer Phase=Ready transition. Without this the next
		// Tunnel can race past EligibleExits while the exit still
		// looks Provisioning in cache, causing the scheduler to
		// provision a second exit instead of binpacking.
		time.Sleep(3 * time.Second)

		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata: {name: %s, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 81, targetPort: 8081, protocol: TCP}]
  selector: {app: sched-b}
`, svcB)))).To(Succeed())
	})

	AfterAll(func() {
		_, _ = runKC("delete", "svc", svcA, svcB, "sched-c", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "deploy", "sched-a-be", "sched-b-be", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", "default", "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
		Eventually(func() string {
			out, _ := runKC("get", "tunnel,exitserver", "-n", "default",
				"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
			return strings.TrimSpace(out)
		}, 60*time.Second, 2*time.Second).Should(BeEmpty())
	})

	It("schedules both tunnels onto a single ExitServer", func() {
		By("waiting for both Tunnels to reach Ready")
		for _, name := range []string{svcA, svcB} {
			Eventually(func() string {
				out, _ := runKC("get", "tunnel", name, "-n", "default",
					"-o", "jsonpath={.status.phase}")
				return out
			}).Should(Equal("Ready"))
		}

		By("asserting both tunnels share the same assignedExit")
		exitA, _ := runKC("get", "tunnel", svcA, "-n", "default",
			"-o", "jsonpath={.status.assignedExit}")
		exitB, _ := runKC("get", "tunnel", svcB, "-n", "default",
			"-o", "jsonpath={.status.assignedExit}")
		Expect(exitA).NotTo(BeEmpty())
		Expect(exitA).To(Equal(exitB))

		By("asserting the exit lists exactly one ExitServer with both ports allocated")
		out, _ := runKC("get", "exitserver", "-n", "default",
			"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
		Expect(strings.Fields(out)).To(HaveLen(1))

		alloc80, _ := runKC("get", "exitserver", exitA, "-n", "default",
			"-o", `jsonpath={.status.allocations.80}`)
		alloc81, _ := runKC("get", "exitserver", exitA, "-n", "default",
			"-o", `jsonpath={.status.allocations.81}`)
		Expect(alloc80).NotTo(BeEmpty())
		Expect(alloc81).NotTo(BeEmpty())
	})

	It("does not provision an exit when policy AllowPorts excludes the requested port", func() {
		By("narrowing the policy default AllowPorts to exclude port 22")
		_, err := runKC("patch", "schedulingpolicy", "default", "--type=merge", "-p",
			`{"spec":{"vps":{"default":{"allowPorts":["1024-65535"]}}}}`)
		Expect(err).NotTo(HaveOccurred())

		By("recording exit count before applying the new tunnel")
		out, _ := runKC("get", "exitserver", "-n", "default",
			"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
		exitsBefore := len(strings.Fields(out))

		By("applying a Service requesting a sub-1024 port")
		Expect(applyManifestKC([]byte(`
apiVersion: v1
kind: Service
metadata: {name: sched-c, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: ssh, port: 22, targetPort: 22, protocol: TCP}]
  selector: {app: nonexistent}
`))).To(Succeed())

		By("waiting for ServiceWatcher to create the Tunnel")
		Eventually(func() error {
			_, e := runKC("get", "tunnel", "sched-c", "-n", "default")
			return e
		}, 30*time.Second, 2*time.Second).Should(Succeed())

		By("asserting the Tunnel stays in Allocating for 30s")
		Consistently(func() string {
			out, _ := runKC("get", "tunnel", "sched-c", "-n", "default",
				"-o", "jsonpath={.status.phase}")
			return out
		}, 30*time.Second, 5*time.Second).Should(Equal("Allocating"))

		By("asserting no new ExitServer was created")
		out, _ = runKC("get", "exitserver", "-n", "default",
			"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
		Expect(strings.Fields(out)).To(HaveLen(exitsBefore))

		By("cleaning up sched-c")
		_, _ = runKC("delete", "svc", "sched-c", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "tunnel", "sched-c", "-n", "default", "--ignore-not-found", "--wait=false")
	})
})

var _ = Describe("ServiceWatcher reverse-sync", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	const rsyncSvc = "rsync-svc"
	const rsyncBackend = "rsync-backend"

	BeforeAll(func() {
		Eventually(func() string {
			out, _ := runKC("get", "tunnel,exitserver", "-n", "default",
				"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
			return strings.TrimSpace(out)
		}, 60*time.Second, 2*time.Second).Should(BeEmpty())

		Expect(applyManifestKC([]byte(`
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata: {name: default}
spec:
  consolidation: {reclaimEmpty: false}
  vps:
    default:
      provider: local-docker
      allowPorts: ["80", "1024-65535"]
`))).To(Succeed())

		Expect(applyManifestKC([]byte(`
apiVersion: v1
kind: Secret
metadata: {name: local-docker-credentials, namespace: default}
type: Opaque
stringData: {token: rsync-unused}
`))).To(Succeed())

		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata: {name: %s, namespace: default}
spec:
  replicas: 1
  selector: {matchLabels: {app: %s}}
  template:
    metadata: {labels: {app: %s}}
    spec:
      containers:
        - name: http-echo
          image: hashicorp/http-echo
          args: ["-text=rsync", "-listen=:8080"]
          ports: [{containerPort: 8080}]
`, rsyncBackend, rsyncBackend, rsyncBackend)))).To(Succeed())

		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata: {name: %s, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 80, targetPort: 8080, protocol: TCP}]
  selector: {app: %s}
`, rsyncSvc, rsyncBackend)))).To(Succeed())
	})

	AfterAll(func() {
		_, _ = runKC("delete", "svc", rsyncSvc, "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "deploy", rsyncBackend, "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", "default", "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
		Eventually(func() string {
			out, _ := runKC("get", "tunnel,exitserver", "-n", "default",
				"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
			return strings.TrimSpace(out)
		}, 60*time.Second, 2*time.Second).Should(BeEmpty())
	})

	It("reflects the assigned ExitServer.publicIP into Service.status", func() {
		By("waiting for the Tunnel to reach Ready")
		Eventually(func() string {
			out, _ := runKC("get", "tunnel", rsyncSvc, "-n", "default",
				"-o", "jsonpath={.status.phase}")
			return out
		}).Should(Equal("Ready"))

		By("reading the assigned exit and its publicIP")
		exit, _ := runKC("get", "tunnel", rsyncSvc, "-n", "default",
			"-o", "jsonpath={.status.assignedExit}")
		Expect(exit).NotTo(BeEmpty())
		exitIP, _ := runKC("get", "exitserver", exit, "-n", "default",
			"-o", "jsonpath={.status.publicIP}")
		Expect(exitIP).NotTo(BeEmpty())

		By("waiting for Service.status.loadBalancer.ingress[0].ip to equal the exit's publicIP")
		Eventually(func() string {
			out, _ := runKC("get", "svc", rsyncSvc, "-n", "default",
				"-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
			return strings.TrimSpace(out)
		}).Should(Equal(exitIP))
	})
})

var _ = Describe("Resilience", Ordered, func() {
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(3 * time.Second)

	const resSvc = "res-svc"
	const resBackend = "res-backend"
	const resBody = "res-hello"

	BeforeAll(func() {
		Eventually(func() string {
			out, _ := runKC("get", "tunnel,exitserver", "-n", "default",
				"-o", "jsonpath={range .items[*]}{.metadata.name} {end}")
			return strings.TrimSpace(out)
		}, 60*time.Second, 2*time.Second).Should(BeEmpty())

		Expect(applyManifestKC([]byte(`
apiVersion: frp.operator.io/v1alpha1
kind: SchedulingPolicy
metadata: {name: default}
spec:
  consolidation: {reclaimEmpty: false}
  vps:
    default:
      provider: local-docker
      allowPorts: ["80", "1024-65535"]
`))).To(Succeed())

		Expect(applyManifestKC([]byte(`
apiVersion: v1
kind: Secret
metadata: {name: local-docker-credentials, namespace: default}
type: Opaque
stringData: {token: res-unused}
`))).To(Succeed())

		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata: {name: %s, namespace: default}
spec:
  replicas: 1
  selector: {matchLabels: {app: %s}}
  template:
    metadata: {labels: {app: %s}}
    spec:
      containers:
        - name: http-echo
          image: hashicorp/http-echo
          args: ["-text=%s", "-listen=:8080"]
          ports: [{containerPort: 8080}]
`, resBackend, resBackend, resBackend, resBody)))).To(Succeed())

		Expect(applyManifestKC([]byte(fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata: {name: %s, namespace: default}
spec:
  type: LoadBalancer
  loadBalancerClass: frp-operator.io/frp
  ports: [{name: http, port: 80, targetPort: 8080, protocol: TCP}]
  selector: {app: %s}
`, resSvc, resBackend)))).To(Succeed())

		By("waiting for Tunnel Ready before chaos")
		Eventually(func() string {
			out, _ := runKC("get", "tunnel", resSvc, "-n", "default",
				"-o", "jsonpath={.status.phase}")
			return out
		}).Should(Equal("Ready"))
	})

	AfterAll(func() {
		_, _ = runKC("delete", "svc", resSvc, "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "deploy", resBackend, "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=true")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", "default", "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
	})

	It("frpc reconnects after frps container restart", func() {
		exit, _ := runKC("get", "tunnel", resSvc, "-n", "default",
			"-o", "jsonpath={.status.assignedExit}")
		Expect(exit).NotTo(BeEmpty())
		exitIP, _ := runKC("get", "exitserver", exit, "-n", "default",
			"-o", "jsonpath={.status.publicIP}")
		Expect(exitIP).NotTo(BeEmpty())

		node := fmt.Sprintf(ldKindNodeFmt, ldKindCluster)

		curl := func() string {
			out, err := utils.Run(exec.Command("docker", "exec", node,
				"curl", "-s", "--max-time", "5", "http://"+exitIP+":80"))
			if err != nil {
				return ""
			}
			return strings.TrimSpace(out)
		}

		By("verifying baseline curl works")
		Eventually(curl).Should(Equal(resBody))

		By("restarting the frps container")
		_, err := utils.Run(exec.Command("docker", "restart",
			"frp-operator-default__"+exit))
		Expect(err).NotTo(HaveOccurred())

		By("waiting up to 90s for traffic to recover")
		Eventually(curl, 90*time.Second, 3*time.Second).Should(Equal(resBody))
	})
})
