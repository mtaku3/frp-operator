# Phase 2: FRP Primitives Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Build the four pure-Go primitives the upcoming controllers will use to talk to FRP and bootstrap a VPS — none of which require Kubernetes, controllers, or a real cloud. Each primitive is a self-contained, table-tested package.

**Architecture:** Four small packages, no dependency between them other than `release` providing version metadata to `bootstrap`.

```
internal/frp/release/    — pinned frps version, URL, SHA-256
internal/frp/config/     — Go types for frpc + frps TOML, render to []byte
internal/frp/admin/      — REST client for frps's webServer admin API
internal/bootstrap/      — cloud-init user-data template + renderer
```

**Tech Stack:** Go 1.24+, stdlib `net/http` + `httptest`, `text/template`, `github.com/BurntSushi/toml` for marshalling (already widely used in the FRP ecosystem and Kubebuilder ships it transitively; if not in `go.mod`, this plan adds it).

**Reference spec:** [`docs/superpowers/specs/2026-04-23-frp-operator-design.md`](../specs/2026-04-23-frp-operator-design.md) §6 (controllers — these primitives are what controllers will call), §7 (bootstrap and auth).

**Out of scope:** Controller integration, CRD changes, provider impls, scheduler logic. All that lands in Phase 5+.

---

## File Structure

```
internal/frp/release/release.go          # Version, URL, SHA256 constants + DownloadURL() helper
internal/frp/release/release_test.go     # Sanity tests on URL composition

internal/frp/config/frpc.go              # FrpcConfig struct + Render() → []byte (TOML)
internal/frp/config/frps.go              # FrpsConfig struct + Render() → []byte (TOML)
internal/frp/config/frpc_test.go         # Golden-file render tests
internal/frp/config/frps_test.go         # Golden-file render tests
internal/frp/config/testdata/            # Golden TOML files

internal/frp/admin/client.go             # Client struct + ServerInfo, Reload, PutConfig, ListProxies
internal/frp/admin/types.go              # Response/request types matching frps API
internal/frp/admin/client_test.go        # Tests against httptest.Server

internal/bootstrap/cloudinit.go          # Render(BootstrapInput) → []byte (cloud-init YAML)
internal/bootstrap/cloudinit.tmpl        # Embedded text/template
internal/bootstrap/cloudinit_test.go     # Golden-file render tests
internal/bootstrap/testdata/             # Golden cloud-init YAML
```

**Boundaries.** `release` is pure constants. `config` has no I/O — pure data → bytes. `admin` is HTTP-only, no Kubernetes. `bootstrap` depends only on `release` (for the FRP URL/checksum). Controllers (future phases) compose these.

---

## Task 1: FRP release metadata

**Files:**
- Create: `internal/frp/release/release.go`
- Test: `internal/frp/release/release_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/frp/release/release_test.go`:

```go
package release

import (
	"strings"
	"testing"
)

func TestDownloadURLLinuxAmd64(t *testing.T) {
	url := DownloadURL("linux", "amd64")
	if !strings.HasPrefix(url, "https://github.com/fatedier/frp/releases/download/") {
		t.Errorf("unexpected host/path: %s", url)
	}
	if !strings.Contains(url, Version) {
		t.Errorf("URL missing version %q: %s", Version, url)
	}
	if !strings.Contains(url, "linux_amd64") {
		t.Errorf("URL missing platform: %s", url)
	}
	if !strings.HasSuffix(url, ".tar.gz") {
		t.Errorf("URL missing extension: %s", url)
	}
}

func TestVersionAndChecksumPopulated(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
	if !strings.HasPrefix(Version, "v") {
		t.Errorf("Version must start with v, got %q", Version)
	}
	if SHA256LinuxAmd64 == "" || len(SHA256LinuxAmd64) != 64 {
		t.Errorf("SHA256LinuxAmd64 must be a 64-char hex string, got %q (len=%d)",
			SHA256LinuxAmd64, len(SHA256LinuxAmd64))
	}
}
```

- [ ] **Step 2: Run test, confirm FAIL**

Run: `devbox run -- go test ./internal/frp/release/ -v`
Expected: build fails — package doesn't exist.

- [ ] **Step 3: Write `internal/frp/release/release.go`**

```go
// Package release exposes the pinned upstream FRP release the operator ships
// to provisioned VPSes. The version, URLs, and checksums change in lockstep
// with the operator binary — they are not user-configurable at runtime.
package release

import "fmt"

// Version is the upstream FRP release tag bundled with this operator build.
// Changing it requires updating the matching SHA256 constants below.
const Version = "v0.65.0"

// SHA256 checksums are taken from the upstream release page's checksums.txt.
// The implementer should verify these against the live release before
// committing — substitute the real values from
// https://github.com/fatedier/frp/releases/download/<Version>/checksums.txt
const (
	SHA256LinuxAmd64 = "REPLACE_WITH_REAL_SHA256_64_HEX_CHARS_AAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	SHA256LinuxArm64 = "REPLACE_WITH_REAL_SHA256_64_HEX_CHARS_BBBBBBBBBBBBBBBBBBBBBBBBBBBB"
)

// DownloadURL returns the upstream tarball URL for the pinned Version.
// goos must be one of: linux, darwin. goarch must be one of: amd64, arm64.
// Other combinations are rejected at the caller.
func DownloadURL(goos, goarch string) string {
	return fmt.Sprintf(
		"https://github.com/fatedier/frp/releases/download/%s/frp_%s_%s_%s.tar.gz",
		Version, strings.TrimPrefix(Version, "v"), goos, goarch,
	)
}
```

The implementer **must** then:

1. Fetch the real SHA256 values for `Version` from upstream:
   ```bash
   curl -sL https://github.com/fatedier/frp/releases/download/v0.65.0/checksums.txt
   ```
2. Replace the `REPLACE_WITH_REAL_SHA256_*` placeholders with the real 64-char hex SHA-256s for `frp_0.65.0_linux_amd64.tar.gz` and `frp_0.65.0_linux_arm64.tar.gz`.
3. If `v0.65.0` does not exist upstream (download returns 404), pick the highest available `v0.6x.y` release and update Version + both checksums together. **Report the version chosen in the implementer report.**

You also need to add the `strings` import. (The plan deliberately leaves the import omitted so the test that compiles requires you to fix it — keeps the TDD honest.)

- [ ] **Step 4: Run tests, confirm PASS**

Run: `devbox run -- go test ./internal/frp/release/ -v`
Expected: both tests pass. If the download URL fails (404), update Version and re-run.

- [ ] **Step 5: Commit**

```bash
git add internal/frp/release/
git commit -m "feat(frp/release): pin upstream FRP version with SHA-256 checksums"
```

---

## Task 2: frpc TOML config rendering

**Files:**
- Create: `internal/frp/config/frpc.go`
- Test: `internal/frp/config/frpc_test.go`
- Test data: `internal/frp/config/testdata/frpc_minimal.toml`, `internal/frp/config/testdata/frpc_multi_proxy.toml`
- Modify: `go.mod`, `go.sum` (adds `github.com/BurntSushi/toml` if not already present)

- [ ] **Step 1: Verify TOML library**

Run: `grep -F BurntSushi go.mod`. If empty:
```bash
devbox run -- go get github.com/BurntSushi/toml@latest
```
Otherwise it's already a transitive dep — promote to direct via `go mod tidy` after the first import lands.

- [ ] **Step 2: Write the failing test**

Create `internal/frp/config/frpc_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderFrpcMinimal(t *testing.T) {
	cfg := FrpcConfig{
		ServerAddr: "203.0.113.10",
		ServerPort: 7000,
		Auth: FrpcAuth{Method: "token", Token: "secret-token-1234"},
		Proxies: []FrpcProxy{
			{
				Name:       "my-ns_my-tunnel_http",
				Type:       "tcp",
				LocalIP:    "10.0.0.42",
				LocalPort:  80,
				RemotePort: 80,
			},
		},
	}
	got, err := cfg.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := readGolden(t, "frpc_minimal.toml")
	if normalize(string(got)) != normalize(want) {
		t.Errorf("frpc_minimal output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderFrpcMultiProxy(t *testing.T) {
	cfg := FrpcConfig{
		ServerAddr: "203.0.113.20",
		ServerPort: 7000,
		Auth:       FrpcAuth{Method: "token", Token: "tok"},
		Proxies: []FrpcProxy{
			{Name: "ns_t1_http", Type: "tcp", LocalIP: "10.0.0.1", LocalPort: 80, RemotePort: 80},
			{Name: "ns_t1_https", Type: "tcp", LocalIP: "10.0.0.1", LocalPort: 443, RemotePort: 443},
			{Name: "ns_t2_pg", Type: "tcp", LocalIP: "10.0.0.2", LocalPort: 5432, RemotePort: 5432},
		},
	}
	got, err := cfg.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := readGolden(t, "frpc_multi_proxy.toml")
	if normalize(string(got)) != normalize(want) {
		t.Errorf("frpc_multi_proxy output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	return string(b)
}

// normalize removes trailing whitespace per line and trims leading/trailing
// blank lines so golden comparisons are tolerant of TOML serializer quirks.
func normalize(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n") + "\n"
}
```

- [ ] **Step 3: Run test, confirm FAIL**

Run: `devbox run -- go test ./internal/frp/config/ -run TestRenderFrpc -v`
Expected: build fails — types undefined.

- [ ] **Step 4: Write `internal/frp/config/frpc.go`**

```go
// Package config renders FRP client and server TOML configuration. Pure
// data-in / bytes-out; no I/O. Controllers in later phases compose these
// outputs into Secrets, ConfigMaps, or cloud-init payloads.
package config

import (
	"bytes"

	"github.com/BurntSushi/toml"
)

// FrpcAuth carries the client-side authentication settings. Method is
// typically "token" with a corresponding shared Token.
type FrpcAuth struct {
	Method string `toml:"method,omitempty"`
	Token  string `toml:"token,omitempty"`
}

// FrpcProxy is one proxy entry written into the [[proxies]] array of frpc.toml.
// Name must be globally unique on the targeted frps; the operator namespaces
// it as "<tenant-ns>_<tunnel-name>_<port-name>".
type FrpcProxy struct {
	Name       string `toml:"name"`
	Type       string `toml:"type"` // "tcp" or "udp"
	LocalIP    string `toml:"localIP"`
	LocalPort  int    `toml:"localPort"`
	RemotePort int    `toml:"remotePort"`
}

// FrpcConfig is the in-memory representation of frpc.toml.
type FrpcConfig struct {
	ServerAddr string      `toml:"serverAddr"`
	ServerPort int         `toml:"serverPort"`
	Auth       FrpcAuth    `toml:"auth,omitempty"`
	Proxies    []FrpcProxy `toml:"proxies,omitempty"`
}

// Render encodes the config as TOML bytes suitable for /etc/frp/frpc.toml or
// for mounting into an frpc Pod via a Secret.
func (c FrpcConfig) Render() ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 5: Run, capture actual output, write golden file**

```bash
devbox run -- go test ./internal/frp/config/ -run TestRenderFrpc -v
```

The test will FAIL the first time because `testdata/` doesn't exist. **Capture the rendered output from the failure message** (the test prints "got"). Manually copy that output into `internal/frp/config/testdata/frpc_minimal.toml` and `internal/frp/config/testdata/frpc_multi_proxy.toml`, ONE PER FAILED CASE.

Inspect each golden file by hand and verify it's plausibly correct frpc TOML:
- Has `serverAddr = "203.0.113.10"` and `serverPort = 7000` at top level.
- `[auth]` block (or `auth.method`/`auth.token` keys) with the right values.
- `[[proxies]]` array with one entry per proxy, fields `name`/`type`/`localIP`/`localPort`/`remotePort`.

- [ ] **Step 6: Re-run, confirm PASS**

```bash
devbox run -- go test ./internal/frp/config/ -v
```
Expected: both tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/frp/config/frpc.go internal/frp/config/frpc_test.go internal/frp/config/testdata/ go.mod go.sum
git commit -m "feat(frp/config): render frpc.toml from FrpcConfig with golden tests"
```

---

## Task 3: frps TOML config rendering

**Files:**
- Create: `internal/frp/config/frps.go`
- Test: `internal/frp/config/frps_test.go`
- Test data: `internal/frp/config/testdata/frps_default.toml`

- [ ] **Step 1: Write the failing test**

Create `internal/frp/config/frps_test.go`:

```go
package config

import "testing"

func TestRenderFrpsDefault(t *testing.T) {
	cfg := FrpsConfig{
		BindPort: 7000,
		Auth:     FrpsAuth{Method: "token", Token: "exit-token-xyz"},
		WebServer: FrpsWebServer{
			Addr:     "0.0.0.0",
			Port:     7500,
			User:     "admin",
			Password: "admin-password-xyz",
		},
		AllowPorts: []FrpsPortRange{
			{Single: 80},
			{Single: 443},
			{Start: 1024, End: 65535},
		},
	}
	got, err := cfg.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := readGolden(t, "frps_default.toml")
	if normalize(string(got)) != normalize(want) {
		t.Errorf("frps_default output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run, confirm FAIL**

Run: `devbox run -- go test ./internal/frp/config/ -run TestRenderFrps -v`
Expected: build fails — types undefined.

- [ ] **Step 3: Write `internal/frp/config/frps.go`**

```go
package config

import (
	"bytes"
	"fmt"

	"github.com/BurntSushi/toml"
)

// FrpsAuth carries server-side auth settings. Mirrors FrpcAuth so a matched
// pair (same Method+Token) authenticates a client to a server.
type FrpsAuth struct {
	Method string `toml:"method,omitempty"`
	Token  string `toml:"token,omitempty"`
}

// FrpsWebServer configures frps's admin REST API endpoint. The operator
// connects to <Addr>:<Port> using HTTP basic auth (User/Password) to push
// config and reload.
type FrpsWebServer struct {
	Addr     string `toml:"addr"`
	Port     int    `toml:"port"`
	User     string `toml:"user,omitempty"`
	Password string `toml:"password,omitempty"`
}

// FrpsPortRange is either a single port (Single != 0) or a [Start, End]
// range. FRP's allowPorts TOML expects strings like "80" or "1024-65535";
// MarshalTOML below produces those.
type FrpsPortRange struct {
	Single int
	Start  int
	End    int
}

// MarshalTOML implements toml.Marshaler so FrpsPortRange serializes as the
// FRP-expected string form. (frp's config schema for allowPorts is an
// array of {single = 80} or {start = 1024, end = 65535} tables in modern
// versions; the operator uses the table form for clarity and round-trip.)
func (p FrpsPortRange) MarshalTOML() ([]byte, error) {
	if p.Single != 0 {
		return []byte(fmt.Sprintf("{ single = %d }", p.Single)), nil
	}
	return []byte(fmt.Sprintf("{ start = %d, end = %d }", p.Start, p.End)), nil
}

// FrpsConfig is the in-memory representation of frps.toml.
type FrpsConfig struct {
	BindPort   int             `toml:"bindPort"`
	Auth       FrpsAuth        `toml:"auth,omitempty"`
	WebServer  FrpsWebServer   `toml:"webServer,omitempty"`
	AllowPorts []FrpsPortRange `toml:"allowPorts,omitempty"`
}

// Render encodes the config as TOML bytes suitable for /etc/frp/frps.toml.
func (c FrpsConfig) Render() ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 4: Run, capture output, write golden file**

```bash
devbox run -- go test ./internal/frp/config/ -run TestRenderFrps -v
```
Capture the rendered `got` from the failure and save as `testdata/frps_default.toml`. Verify it's plausibly correct frps TOML: has `bindPort = 7000`, `[auth]` block, `[webServer]` block with addr/port/user/password, and `allowPorts = [...]` containing the three entries.

- [ ] **Step 5: Re-run, confirm PASS**

```bash
devbox run -- go test ./internal/frp/config/ -v
```
Expected: all three render tests pass (frpc_minimal, frpc_multi_proxy, frps_default).

- [ ] **Step 6: Commit**

```bash
git add internal/frp/config/frps.go internal/frp/config/frps_test.go internal/frp/config/testdata/frps_default.toml
git commit -m "feat(frp/config): render frps.toml from FrpsConfig"
```

---

## Task 4: frps admin REST client

**Files:**
- Create: `internal/frp/admin/client.go`
- Create: `internal/frp/admin/types.go`
- Test: `internal/frp/admin/client_test.go`

The admin client wraps frps's webServer REST endpoints. The operator uses it for: health probe (`/api/serverinfo`), config push (`PUT /api/config`), reload (`GET /api/reload`), and proxy listing (`GET /api/proxy/tcp` / `udp` for status reconciliation).

- [ ] **Step 1: Write the failing test**

Create `internal/frp/admin/client_test.go`:

```go
package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/serverinfo" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if u, p, ok := r.BasicAuth(); !ok || u != "admin" || p != "secret" {
			t.Errorf("missing or wrong basic auth: %v %v %v", u, p, ok)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ServerInfo{
			Version:        "0.65.0",
			BindPort:       7000,
			ProxyCounts:    map[string]int{"tcp": 3, "udp": 0},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	info, err := c.ServerInfo(context.Background())
	if err != nil {
		t.Fatalf("ServerInfo: %v", err)
	}
	if info.Version != "0.65.0" {
		t.Errorf("Version: got %q", info.Version)
	}
	if info.ProxyCounts["tcp"] != 3 {
		t.Errorf("ProxyCounts[tcp]: got %d, want 3", info.ProxyCounts["tcp"])
	}
}

func TestPutConfigSendsBodyAndReloads(t *testing.T) {
	var seenPaths []string
	var seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/config":
			b, _ := io.ReadAll(r.Body)
			seenBody = string(b)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/api/reload":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "secret")
	body := []byte(`bindPort = 7000` + "\n" + `[auth]` + "\n" + `token = "x"` + "\n")
	if err := c.PutConfigAndReload(context.Background(), body); err != nil {
		t.Fatalf("PutConfigAndReload: %v", err)
	}
	want := []string{"PUT /api/config", "GET /api/reload"}
	if len(seenPaths) != 2 || seenPaths[0] != want[0] || seenPaths[1] != want[1] {
		t.Errorf("call sequence: got %v want %v", seenPaths, want)
	}
	if !strings.Contains(seenBody, "bindPort = 7000") {
		t.Errorf("body not forwarded: %q", seenBody)
	}
}

func TestServerInfoBadAuthReturnsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "admin", "wrong")
	_, err := c.ServerInfo(context.Background())
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error should mention 401: %v", err)
	}
}
```

- [ ] **Step 2: Run, confirm FAIL**

Run: `devbox run -- go test ./internal/frp/admin/ -v`
Expected: build fails — types undefined.

- [ ] **Step 3: Write `internal/frp/admin/types.go`**

```go
package admin

// ServerInfo is the response from GET /api/serverinfo on frps. Field set
// is intentionally minimal; expand as later phases need more.
type ServerInfo struct {
	Version     string         `json:"version"`
	BindPort    int            `json:"bind_port"`
	ProxyCounts map[string]int `json:"proxy_type_count,omitempty"`
}

// Proxy describes one proxy entry from GET /api/proxy/<type>. Used for
// reconciliation: the operator compares its desired allocations against
// what frps actually reports.
type Proxy struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	RemotePort int    `json:"remote_port"`
	TodayInGB  int64  `json:"today_traffic_in,omitempty"`
	TodayOutGB int64  `json:"today_traffic_out,omitempty"`
}
```

- [ ] **Step 4: Write `internal/frp/admin/client.go`**

```go
// Package admin is a thin REST client for frps's webServer admin API.
// All methods accept a context and surface non-2xx responses as errors.
package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client targets one frps instance.
type Client struct {
	BaseURL    string
	User       string
	Password   string
	HTTPClient *http.Client
}

// NewClient returns a Client with a 10s default HTTP timeout. Callers may
// override HTTPClient afterwards (e.g., longer timeouts for /api/config).
func NewClient(baseURL, user, password string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		User:       user,
		Password:   password,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// ServerInfo calls GET /api/serverinfo.
func (c *Client) ServerInfo(ctx context.Context) (*ServerInfo, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/serverinfo", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info ServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode serverinfo: %w", err)
	}
	return &info, nil
}

// PutConfigAndReload pushes a new frps.toml body via PUT /api/config, then
// triggers GET /api/reload. Both steps must succeed; if reload fails the
// caller should inspect the most recent ServerInfo to detect partial state.
func (c *Client) PutConfigAndReload(ctx context.Context, configBody []byte) error {
	if _, err := c.do(ctx, http.MethodPut, "/api/config", bytes.NewReader(configBody)); err != nil {
		return fmt.Errorf("put config: %w", err)
	}
	if _, err := c.do(ctx, http.MethodGet, "/api/reload", nil); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	return nil
}

// ListProxies calls GET /api/proxy/<proxyType> ("tcp" or "udp"). Returns
// the raw Proxy list; status interpretation is the caller's job.
func (c *Client) ListProxies(ctx context.Context, proxyType string) ([]Proxy, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/proxy/"+proxyType, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var wrap struct {
		Proxies []Proxy `json:"proxies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrap); err != nil {
		return nil, fmt.Errorf("decode proxies: %w", err)
	}
	return wrap.Proxies, nil
}

// do builds, sends, and validates an HTTP request. The body is consumed
// here for non-2xx responses so callers don't leak connections.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.User != "" {
		req.SetBasicAuth(c.User, c.Password)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		drain, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s %s: %s: %s",
			method, path, resp.Status, strings.TrimSpace(string(drain)))
	}
	return resp, nil
}
```

- [ ] **Step 5: Run, confirm PASS**

Run: `devbox run -- go test ./internal/frp/admin/ -v`
Expected: 3 tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/frp/admin/
git commit -m "feat(frp/admin): REST client for frps webServer API"
```

---

## Task 5: cloud-init bootstrap renderer

**Files:**
- Create: `internal/bootstrap/cloudinit.go`
- Create: `internal/bootstrap/cloudinit.tmpl`
- Test: `internal/bootstrap/cloudinit_test.go`
- Test data: `internal/bootstrap/testdata/cloudinit_basic.yaml`

This produces the user-data passed to DigitalOcean (and other providers) at droplet-create time. The script installs `frps`, writes its config, opens a firewall, and starts a systemd unit.

- [ ] **Step 1: Write the failing test**

Create `internal/bootstrap/cloudinit_test.go`:

```go
package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderCloudInitBasic(t *testing.T) {
	in := Input{
		FrpsConfigTOML:  []byte("bindPort = 7000\n[auth]\ntoken = \"abc\"\n"),
		AdminPort:       7500,
		BindPort:        7000,
		AllowPortsRange: "1024-65535",
		ReservedPorts:   []int{22, 7000, 7500},
		FrpsVersion:     "v0.65.0",
		FrpsDownloadURL: "https://github.com/fatedier/frp/releases/download/v0.65.0/frp_0.65.0_linux_amd64.tar.gz",
		FrpsSHA256:      "abc1234567890123456789012345678901234567890123456789012345678901",
	}
	got, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "cloudinit_basic.yaml"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderCloudInitContainsExpectedKeys(t *testing.T) {
	in := Input{
		FrpsConfigTOML:  []byte("bindPort = 7000\n"),
		AdminPort:       7500,
		BindPort:        7000,
		AllowPortsRange: "1024-65535",
		ReservedPorts:   []int{22, 7000, 7500},
		FrpsVersion:     "v0.65.0",
		FrpsDownloadURL: "https://example.test/frp.tar.gz",
		FrpsSHA256:      "0000000000000000000000000000000000000000000000000000000000000000",
	}
	got, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(got)
	for _, want := range []string{
		"#cloud-config",
		"runcmd:",
		"https://example.test/frp.tar.gz",
		"sha256sum",
		"frps.service",
		"ufw allow 7000",
		"ufw allow 7500",
		"ufw allow 1024:65535",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered cloud-init missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run, confirm FAIL**

Run: `devbox run -- go test ./internal/bootstrap/ -v`
Expected: build fails — types undefined.

- [ ] **Step 3: Write `internal/bootstrap/cloudinit.tmpl`**

This is a `text/template` source file. Critically, embed the TOML config via base64 to avoid quoting hell (cloud-init `write_files` accepts a `content` field, but multi-line TOML inside YAML is fragile; base64-encoded `content` with `encoding: b64` is robust).

```
#cloud-config
package_update: true
package_upgrade: false

write_files:
  - path: /etc/frp/frps.toml
    permissions: "0600"
    owner: root:root
    encoding: b64
    content: {{ .FrpsConfigB64 }}
  - path: /etc/systemd/system/frps.service
    permissions: "0644"
    owner: root:root
    content: |
      [Unit]
      Description=FRP server (frps)
      After=network-online.target
      Wants=network-online.target

      [Service]
      Type=simple
      ExecStart=/usr/local/bin/frps -c /etc/frp/frps.toml
      Restart=always
      RestartSec=5

      [Install]
      WantedBy=multi-user.target

runcmd:
  - set -eux
  - cd /tmp
  - curl -fsSL -o frp.tar.gz {{ .FrpsDownloadURL }}
  - echo "{{ .FrpsSHA256 }}  frp.tar.gz" | sha256sum -c -
  - tar -xzf frp.tar.gz --strip-components=1 -C /usr/local/bin frp_{{ trimv .FrpsVersion }}_linux_amd64/frps
  - chmod +x /usr/local/bin/frps
  - ufw --force enable
  - ufw allow {{ .BindPort }}/tcp
  - ufw allow {{ .AdminPort }}/tcp
  - ufw allow {{ .AllowPortsRangeUFW }}/tcp
{{- range .ReservedPorts }}
  - ufw deny {{ . }}/tcp || true
{{- end }}
  - systemctl daemon-reload
  - systemctl enable --now frps.service
```

- [ ] **Step 4: Write `internal/bootstrap/cloudinit.go`**

```go
// Package bootstrap renders a cloud-init user-data script that installs
// frps on a freshly provisioned VPS and starts it under systemd. Pure
// rendering — no I/O.
package bootstrap

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
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
	if !strings.Contains(in.AllowPortsRange, "-") {
		return fmt.Errorf("AllowPortsRange must be of the form start-end, got %q", in.AllowPortsRange)
	}
	if in.FrpsDownloadURL == "" || in.FrpsSHA256 == "" || in.FrpsVersion == "" {
		return fmt.Errorf("FrpsVersion, FrpsDownloadURL, FrpsSHA256 are all required")
	}
	if len(in.FrpsSHA256) != 64 {
		return fmt.Errorf("FrpsSHA256 must be 64 hex chars, got len %d", len(in.FrpsSHA256))
	}
	return nil
}
```

- [ ] **Step 5: Run TestRenderCloudInitContainsExpectedKeys, confirm PASS**

This test uses substring assertions, so it should pass first try without a golden file.

```bash
devbox run -- go test ./internal/bootstrap/ -run TestRenderCloudInitContainsExpectedKeys -v
```
Expected: PASS.

- [ ] **Step 6: Run TestRenderCloudInitBasic, capture got, write golden**

```bash
devbox run -- go test ./internal/bootstrap/ -run TestRenderCloudInitBasic -v
```
Expected: FAIL on first run because `testdata/cloudinit_basic.yaml` doesn't exist. Capture the rendered `got` and save as `internal/bootstrap/testdata/cloudinit_basic.yaml`. Inspect by hand:
- Starts with `#cloud-config`.
- `write_files` writes `/etc/frp/frps.toml` (base64-encoded) and `/etc/systemd/system/frps.service`.
- `runcmd` downloads `https://github.com/fatedier/frp/releases/download/v0.65.0/frp_0.65.0_linux_amd64.tar.gz`, verifies the sha256, extracts, opens UFW for the right ports, denies reserved ports, enables and starts frps.service.

- [ ] **Step 7: Re-run, confirm both tests PASS**

```bash
devbox run -- go test ./internal/bootstrap/ -v
```
Expected: 2 tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/bootstrap/
git commit -m "feat(bootstrap): render cloud-init user-data for frps install"
```

---

## Task 6: End-to-end primitive composition test

**Files:**
- Create: `internal/bootstrap/integration_test.go`

This is a single integration test that exercises all four packages together: render frps config, render cloud-init using that config + the pinned release, and verify the final cloud-init contains the expected URL/checksum from the release package. It guards against future divergence between the three pieces.

- [ ] **Step 1: Write the test**

```go
package bootstrap_test

import (
	"strings"
	"testing"

	"github.com/mtaku3/frp-operator/internal/bootstrap"
	"github.com/mtaku3/frp-operator/internal/frp/config"
	"github.com/mtaku3/frp-operator/internal/frp/release"
)

func TestComposedRender(t *testing.T) {
	cfg := config.FrpsConfig{
		BindPort: 7000,
		Auth:     config.FrpsAuth{Method: "token", Token: "compose-test-token"},
		WebServer: config.FrpsWebServer{
			Addr: "0.0.0.0", Port: 7500, User: "admin", Password: "compose-pw",
		},
		AllowPorts: []config.FrpsPortRange{{Start: 1024, End: 65535}},
	}
	body, err := cfg.Render()
	if err != nil {
		t.Fatalf("frps render: %v", err)
	}

	out, err := bootstrap.Render(bootstrap.Input{
		FrpsConfigTOML:  body,
		BindPort:        7000,
		AdminPort:       7500,
		AllowPortsRange: "1024-65535",
		ReservedPorts:   []int{22, 7000, 7500},
		FrpsVersion:     release.Version,
		FrpsDownloadURL: release.DownloadURL("linux", "amd64"),
		FrpsSHA256:      release.SHA256LinuxAmd64,
	})
	if err != nil {
		t.Fatalf("bootstrap render: %v", err)
	}

	s := string(out)
	for _, want := range []string{
		"#cloud-config",
		release.Version,
		release.SHA256LinuxAmd64,
		"frps.service",
		"systemctl enable --now frps.service",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("composed render missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run, confirm PASS**

```bash
devbox run -- go test ./internal/bootstrap/ -run TestComposedRender -v
```
Expected: PASS.

- [ ] **Step 3: Run the full Phase 2 test set**

```bash
devbox run -- go test ./internal/...
```
Expected: every package under `internal/...` passes — that's `internal/controller` from Phase 1 plus the four new ones (`internal/frp/release`, `internal/frp/config`, `internal/frp/admin`, `internal/bootstrap`).

- [ ] **Step 4: Commit**

```bash
git add internal/bootstrap/integration_test.go
git commit -m "test(bootstrap): integration check that release+config+bootstrap compose correctly"
```

---

## Phase 2 done — exit criteria

- `devbox run -- go test ./internal/...` passes (Phase 1 controller suite + Phase 2 primitives, all green).
- `internal/frp/release/` exposes `Version`, `SHA256LinuxAmd64`, `SHA256LinuxArm64`, `DownloadURL(os, arch)`.
- `internal/frp/config/` renders frpc.toml and frps.toml from Go structs (golden-tested).
- `internal/frp/admin/` provides `Client` with `ServerInfo`, `PutConfigAndReload`, `ListProxies`. Auth is HTTP basic. Tested against `httptest.Server`.
- `internal/bootstrap/` renders cloud-init user-data (golden-tested).
- No CRD changes, no controller logic, no provider impls.

The next plan (Phase 3: Provisioner interface + LocalDocker provisioner) builds the provider abstraction on top of these primitives. The phase after (Phase 4: Allocator + ProvisionStrategy) is pure-logic and can be developed in parallel with Phase 3 if desired.
