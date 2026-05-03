# E2E Coverage Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add eight new e2e specs covering Tunnel/ExitServer lifecycle, scheduling refusal, binpack reuse, ServiceWatcher reverse-sync, frpc resilience, and webhook validation, split across the existing `e2e_localdocker` (out-of-cluster operator + real frps) and `e2e` (in-cluster operator + cert-manager) suites.

**Architecture:** Six specs extend `test/e2e/localdocker_e2e_test.go` under build tag `e2e_localdocker`, grouped into Lifecycle / Scheduling / ServiceWatcher / Resilience `Describe` blocks for state isolation. Two specs extend `test/e2e/e2e_test.go` under build tag `e2e`, behind an `E2E_WEBHOOK=1` env gate that flips the existing `setupCertManager` scaffolding to install. The existing helper functions (`runKC`, `applyManifestKC`, `utils.Run`) carry over.

**Tech Stack:** Go, Ginkgo/Gomega, kubectl, kind, Docker, cert-manager, frps/frpc.

---

## File Structure

| File | Build tag | Change |
|------|-----------|--------|
| `test/e2e/localdocker_e2e_test.go` | `e2e_localdocker` | Add Describe blocks "Lifecycle", "Scheduling", "ServiceWatcher reverse-sync", "Resilience". |
| `test/e2e/localdocker_suite_test.go` | `e2e_localdocker` | Unchanged. |
| `test/e2e/e2e_test.go` | `e2e` | Add Describe block "Webhook validation" guarded by `E2E_WEBHOOK=1`. |
| `test/e2e/e2e_suite_test.go` | `e2e` | When `E2E_WEBHOOK=1`, do not auto-set `CERT_MANAGER_INSTALL_SKIP=true`. |
| `Makefile` | n/a | Add `test-e2e-webhook` target. |

Each Describe block carries its own resources prefixed by the block name (`lc-…`, `sched-…`, `rsync-…`, `res-…`, `wh-…`) so cleanup is local and specs don't fight over shared CR names.

---

## Task 1: Block A — Tunnel deletion releases ports (A1)

**Files:**
- Modify: `test/e2e/localdocker_e2e_test.go`

- [ ] **Step 1: Add the Lifecycle Describe block at the bottom of the file.**

```go
var _ = Describe("Lifecycle", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	const lcSvc = "lc-svc"
	const lcBackend = "lc-backend"

	BeforeAll(func() {
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
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", "default", "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
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

		By("deleting the Tunnel")
		_, err = runKC("delete", "tunnel", lcSvc, "-n", "default", "--wait=true")
		Expect(err).NotTo(HaveOccurred())

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
})
```

- [ ] **Step 2: Compile the suite.**

Run: `go vet -tags=e2e_localdocker ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Run only the new spec to verify it passes.**

Run: `KIND_CLUSTER=frp-operator-test-e2e KEEP_CLUSTER=1 make test-e2e-localdocker -- -ginkgo.focus="Tunnel deletion releases"` (use the existing `make test-e2e-localdocker`; if Ginkgo focus argument plumbing isn't set up, run `go test -tags=e2e_localdocker ./test/e2e/ -v -ginkgo.v -ginkgo.focus="Tunnel deletion releases"` after exporting the kind kubeconfig).
Expected: `1 Passed`.

- [ ] **Step 4: Commit.**

```bash
git add test/e2e/localdocker_e2e_test.go
git commit -m "test(e2e/localdocker): assert Tunnel deletion releases exit port allocation"
```

---

## Task 2: Block A — ExitServer destroy via finalizer (A2)

**Files:**
- Modify: `test/e2e/localdocker_e2e_test.go`

- [ ] **Step 1: Add a second `It` to the Lifecycle block that runs after A1.**

```go
	It("ExitServer deletion runs the finalizer (container + cred secret gone)", func() {
		By("listing the empty exit left over from A1")
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
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e_localdocker ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Run the Lifecycle block.**

Run: `go test -tags=e2e_localdocker ./test/e2e/ -v -ginkgo.v -ginkgo.focus="Lifecycle"` after exporting kubeconfig.
Expected: `2 Passed`.

- [ ] **Step 4: Commit.**

```bash
git add test/e2e/localdocker_e2e_test.go
git commit -m "test(e2e/localdocker): assert ExitServer finalizer cleans up container and creds"
```

---

## Task 3: Block B — Multi-tunnel binpack onto one exit (B1)

**Files:**
- Modify: `test/e2e/localdocker_e2e_test.go`

- [ ] **Step 1: Add the Scheduling Describe block.**

```go
var _ = Describe("Scheduling", Ordered, func() {
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	const svcA = "sched-a"
	const svcB = "sched-b"

	BeforeAll(func() {
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

		// Two backends sharing one Pod template style; separate Deployments.
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
		_, _ = runKC("delete", "svc", svcA, svcB, "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "deploy", "sched-a-be", "sched-b-be", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", "default", "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
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
})
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e_localdocker ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Run the Scheduling block.**

Run: `go test -tags=e2e_localdocker ./test/e2e/ -v -ginkgo.v -ginkgo.focus="schedules both tunnels"` after exporting kubeconfig.
Expected: `1 Passed`.

- [ ] **Step 4: Commit.**

```bash
git add test/e2e/localdocker_e2e_test.go
git commit -m "test(e2e/localdocker): assert two tunnels binpack onto one exit"
```

---

## Task 4: Block B — AllowPorts refusal (B2)

**Files:**
- Modify: `test/e2e/localdocker_e2e_test.go`

- [ ] **Step 1: Add a second `It` to the Scheduling block.**

The spec runs after B1. We re-use B1's policy, but B1's two tunnels still hold port 80 / 81 on the existing exit. To assert refusal we need a fresh policy that excludes the requested port. Patch the policy in place.

```go
	It("does not provision an exit when policy AllowPorts excludes the requested port", func() {
		By("narrowing the policy default AllowPorts to exclude port 22 (intentional out-of-range)")
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
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e_localdocker ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Run the Scheduling block.**

Run: `go test -tags=e2e_localdocker ./test/e2e/ -v -ginkgo.v -ginkgo.focus="Scheduling"` after exporting kubeconfig.
Expected: `2 Passed`.

- [ ] **Step 4: Commit.**

```bash
git add test/e2e/localdocker_e2e_test.go
git commit -m "test(e2e/localdocker): assert tunnel stays Allocating when policy AllowPorts excludes port"
```

---

## Task 5: Block C — ServiceWatcher reverse-sync (C1)

**Files:**
- Modify: `test/e2e/localdocker_e2e_test.go`

- [ ] **Step 1: Add the ServiceWatcher reverse-sync Describe block.**

```go
var _ = Describe("ServiceWatcher reverse-sync", Ordered, func() {
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	const rsyncSvc = "rsync-svc"
	const rsyncBackend = "rsync-backend"

	BeforeAll(func() {
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
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "secret", "local-docker-credentials", "-n", "default", "--ignore-not-found")
		_, _ = runKC("delete", "schedulingpolicy", "default", "--ignore-not-found")
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
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e_localdocker ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Run the new block.**

Run: `go test -tags=e2e_localdocker ./test/e2e/ -v -ginkgo.v -ginkgo.focus="ServiceWatcher reverse-sync"` after exporting kubeconfig.
Expected: `1 Passed`.

- [ ] **Step 4: Commit.**

```bash
git add test/e2e/localdocker_e2e_test.go
git commit -m "test(e2e/localdocker): assert ServiceWatcher reverse-syncs exit publicIP into Service"
```

---

## Task 6: Block D — frpc reconnect after frps restart (D1)

**Files:**
- Modify: `test/e2e/localdocker_e2e_test.go`

- [ ] **Step 1: Add the Resilience Describe block, gated by env var.**

```go
var _ = Describe("Resilience", Ordered, func() {
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(3 * time.Second)

	const resSvc = "res-svc"
	const resBackend = "res-backend"
	const resBody = "res-hello"

	BeforeAll(func() {
		if os.Getenv("E2E_LOCALDOCKER_RESILIENCE") != "1" {
			Skip("set E2E_LOCALDOCKER_RESILIENCE=1 to run resilience specs")
		}

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
		_, _ = runKC("delete", "tunnel", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
		_, _ = runKC("delete", "exitserver", "--all", "-n", "default", "--ignore-not-found", "--wait=false")
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
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e_localdocker ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Run with the env gate set.**

Run: `E2E_LOCALDOCKER_RESILIENCE=1 go test -tags=e2e_localdocker ./test/e2e/ -v -ginkgo.v -ginkgo.focus="Resilience"` after exporting kubeconfig.
Expected: `1 Passed`. Without the env var: `1 Skipped`.

- [ ] **Step 4: Commit.**

```bash
git add test/e2e/localdocker_e2e_test.go
git commit -m "test(e2e/localdocker): assert frpc reconnects after frps container restart (gated)"
```

---

## Task 7: Run the full localdocker suite

- [ ] **Step 1: Clean prior cluster state.**

Run: `make cleanup-test-e2e || true`

- [ ] **Step 2: Run all localdocker specs.**

Run: `make test-e2e-localdocker`
Expected: 7 Passed (existing 2 + A1, A2, B1, B2, C1) + 1 Skipped (D1).

- [ ] **Step 3: Re-run with resilience enabled.**

Run: `KEEP_CLUSTER=1 make test-e2e-localdocker E2E_LOCALDOCKER_RESILIENCE=1`
Expected: 8 Passed.

- [ ] **Step 4: Tear down kept cluster.**

Run: `make cleanup-test-e2e`

---

## Task 8: Webhook suite — flip cert-manager default behind env

**Files:**
- Modify: `test/e2e/e2e_suite_test.go:54-56`

- [ ] **Step 1: Replace the auto-skip block with an env-gated version.**

Locate the existing block at `test/e2e/e2e_suite_test.go:54-56`:

```go
	if os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "" {
		_ = os.Setenv("CERT_MANAGER_INSTALL_SKIP", "true")
	}
```

Replace with:

```go
	// Default-skip cert-manager unless E2E_WEBHOOK=1 (webhook specs need
	// serving certs). Explicit CERT_MANAGER_INSTALL_SKIP wins over both.
	if os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "" {
		if os.Getenv("E2E_WEBHOOK") == "1" {
			_ = os.Setenv("CERT_MANAGER_INSTALL_SKIP", "false")
		} else {
			_ = os.Setenv("CERT_MANAGER_INSTALL_SKIP", "true")
		}
	}
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Commit.**

```bash
git add test/e2e/e2e_suite_test.go
git commit -m "test(e2e): install cert-manager when E2E_WEBHOOK=1"
```

---

## Task 9: Block E — Tunnel ImmutableWhenReady webhook (E1)

**Files:**
- Modify: `test/e2e/e2e_test.go`

The webhook validator triggers only when `oldT.Spec.ImmutableWhenReady=true && oldT.Status.Phase=Ready`. Real provisioning isn't available in this in-cluster suite (provider stubs), so we set the status manually via the `/status` subresource.

- [ ] **Step 1: Add the Webhook validation Describe block at the bottom of `e2e_test.go`, scoped to `E2E_WEBHOOK=1`.**

```go
var _ = Describe("Webhook validation", Ordered, func() {
	BeforeAll(func() {
		if os.Getenv("E2E_WEBHOOK") != "1" {
			Skip("set E2E_WEBHOOK=1 to run webhook specs (requires cert-manager)")
		}

		// CRDs and operator deployed by the outer Manager BeforeAll.
		// Webhook config is part of `make deploy`; cert-manager mints
		// its serving cert. Wait for the manager to be Ready (already
		// covered by the Manager spec, but the webhook block runs in
		// its own Ordered container so we re-check).
		Eventually(func() error {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Ready", "pod",
				"-l", "control-plane=controller-manager",
				"-n", namespace, "--timeout=120s")
			_, err := utils.Run(cmd)
			return err
		}, 3*time.Minute, 5*time.Second).Should(Succeed())
	})

	AfterAll(func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "tunnel", "wh-immutable",
			"-n", "default", "--ignore-not-found", "--wait=false"))
		_, _ = utils.Run(exec.Command("kubectl", "delete", "exitserver", "wh-grow",
			"-n", "default", "--ignore-not-found", "--wait=false"))
	})

	It("rejects spec change to a Ready+ImmutableWhenReady Tunnel", func() {
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
	})
})
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Commit.**

```bash
git add test/e2e/e2e_test.go
git commit -m "test(e2e/webhook): assert Tunnel ImmutableWhenReady rejects spec change"
```

---

## Task 10: Block E — ExitServer AllowPorts grow-only (E2)

**Files:**
- Modify: `test/e2e/e2e_test.go`

- [ ] **Step 1: Add a second `It` to the Webhook validation block.**

```go
	It("rejects shrinking ExitServer AllowPorts below allocations", func() {
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
	})
```

- [ ] **Step 2: Compile.**

Run: `go vet -tags=e2e ./test/e2e/...`
Expected: no output.

- [ ] **Step 3: Commit.**

```bash
git add test/e2e/e2e_test.go
git commit -m "test(e2e/webhook): assert ExitServer AllowPorts grow-only against allocations"
```

---

## Task 11: Add `make test-e2e-webhook` target

**Files:**
- Modify: `Makefile` after the existing `test-e2e-localdocker` target.

- [ ] **Step 1: Add the target.**

```makefile
.PHONY: test-e2e-webhook
test-e2e-webhook: setup-test-e2e manifests generate fmt vet ## Run the e2e suite with cert-manager + webhook validation specs.
	E2E_WEBHOOK=1 KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) go test -tags=e2e ./test/e2e/ -v -ginkgo.v -timeout=15m; \
	rc=$$?; \
	if [ "$(KEEP_CLUSTER)" != "1" ]; then $(MAKE) cleanup-test-e2e; fi; \
	exit $$rc
```

- [ ] **Step 2: Run the webhook suite end-to-end.**

Run: `make test-e2e-webhook`
Expected: existing `e2e` specs Pass, plus 2 webhook specs Pass.

- [ ] **Step 3: Commit.**

```bash
git add Makefile
git commit -m "build(make): add test-e2e-webhook target wiring cert-manager + webhook specs"
```

---

## Task 12: Final integration run

- [ ] **Step 1: Run all three e2e suites in sequence.**

```bash
make test-e2e
make test-e2e-localdocker
make test-e2e-webhook
```

Expected: all green.

- [ ] **Step 2: Update `test/e2e/README.md` to document the new tags and env vars.**

Modify the "What's covered / What's NOT covered" sections to:

```markdown
## What's covered

### `make test-e2e`
- Manager Pod reaches Ready in the kind cluster.
- Tunnel CR creation triggers reconcile to at least Allocating.
- Service with loadBalancerClass=frp-operator.io/frp triggers ServiceWatcherController to create a sibling Tunnel.
- (with `E2E_WEBHOOK=1`) Tunnel ImmutableWhenReady and ExitServer AllowPorts grow-only validation webhooks reject bad updates.

### `make test-e2e-localdocker`
- ServiceWatcher creates a Tunnel and the operator drives it to Ready against a real frps container.
- Traffic flows kind-node → frps → frpc → backend.
- Tunnel deletion releases its port allocation on the exit.
- ExitServer deletion runs the finalizer (container + cred secret cleaned).
- Two tunnels binpack onto a single ExitServer when AllowPorts permits.
- A tunnel whose requested port falls outside policy AllowPorts stays Allocating; no exit is provisioned.
- Service.status.loadBalancer.ingress reflects the assigned ExitServer.publicIP.
- (with `E2E_LOCALDOCKER_RESILIENCE=1`) frpc reconnects after frps container restart.

## What's NOT covered (deferred)

- DigitalOcean provisioner (requires real DO credentials).
- Migration tests (Tunnel.Spec.MigrationPolicy is declared but unused).
- frpc↔frps network traffic chaos beyond restart (network partition, OOMKill).
- Multi-namespace scenarios.
```

- [ ] **Step 3: Commit.**

```bash
git add test/e2e/README.md
git commit -m "docs(test/e2e): document new specs, tags, and env vars"
```

---

## Self-Review

- All eight specs in spec doc map to tasks: A1=Task 1, A2=Task 2, B1=Task 3, B2=Task 4, C1=Task 5, D1=Task 6, E1=Task 9, E2=Task 10. ✅
- Helpers (`runKC`, `applyManifestKC`, `utils.Run`, `ldKindNodeFmt`, `ldKindCluster`) defined in suite or earlier file — referenced consistently. ✅
- Cert-manager flow uses existing `setupCertManager` plumbing. ✅
- No placeholders, no "similar to Task N" — code repeated where required for cold-read. ✅
- Container name pattern `frp-operator-default__<exit>` matches `localdocker.sanitize` output for `default/<name>` exits. ✅
- D1 gated to avoid CI flake until validated. ✅
