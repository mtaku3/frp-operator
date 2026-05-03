// Package localdocker provisions frps instances as Docker containers on the
// operator's host. Intended for local development and e2e tests, NOT
// production.
//
// Containers are labeled with frp-operator.io/provider=local-docker and
// frp-operator.io/exit=<spec.Name> for cleanup. Ports are published on
// 127.0.0.1 so the operator and external clients can dial them.
//
// The frps.toml content from spec.FrpsConfigTOML is written to a temp file
// and bind-mounted into /etc/frp/frps.toml. The image must contain frps
// configured to read that path; the default image is snowdreamtech/frps,
// which does so.
package localdocker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"

	"github.com/mtaku3/frp-operator/internal/provider"
)

// Config controls the LocalDocker provisioner's runtime behavior. All
// fields are optional; zero values use the documented defaults.
type Config struct {
	// Name is the value Provisioner.Name() returns. Defaults to "local-docker".
	Name string

	// Image is the Docker image reference to run. Defaults to
	// "snowdreamtech/frps:0.68.1".
	Image string

	// HostBindIP controls the address the published ports bind to.
	// Defaults to "127.0.0.1" so the daemon doesn't expose them publicly.
	HostBindIP string

	// Network, if non-empty, attaches the frps container to this Docker
	// network in addition to the default bridge. PublicIP is then reported
	// as the container's IP on that network, so workloads on the same
	// network (e.g., kind nodes/Pods) can dial frps directly without
	// host-port publishing. The network must already exist.
	Network string

	// SkipHostPortPublishing, when true, skips publishing BindPort/AdminPort
	// to host. Useful when callers reach the container via a shared network
	// (e.g., kind) — eliminates host-port collisions across multiple frps
	// instances that share a single bind/admin port.
	SkipHostPortPublishing bool
}

// LocalDocker is a Provisioner that runs frps as Docker containers.
type LocalDocker struct {
	cfg    Config
	client *client.Client
}

// New constructs a LocalDocker. Returns an error if Docker SDK initialization
// fails (e.g., bad DOCKER_HOST). Note that the daemon may still be
// unreachable; callers should Ping or attempt a Create to verify.
func New(cfg Config) (*LocalDocker, error) {
	if cfg.Name == "" {
		cfg.Name = "local-docker"
	}
	if cfg.Image == "" {
		cfg.Image = "snowdreamtech/frps:0.68.1"
	}
	if cfg.HostBindIP == "" {
		cfg.HostBindIP = "127.0.0.1"
	}
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &LocalDocker{cfg: cfg, client: c}, nil
}

// Close releases the Docker SDK client. Idempotent.
func (d *LocalDocker) Close() error {
	if d.client == nil {
		return nil
	}
	err := d.client.Close()
	d.client = nil
	return err
}

// Name implements provider.Provisioner.
func (d *LocalDocker) Name() string { return d.cfg.Name }

// Create writes spec.FrpsConfigTOML to a temp file, pulls the image (if not
// present), and starts a container with the bind-mount and published ports.
// Blocks until the container is running or an error is returned.
func (d *LocalDocker) Create(ctx context.Context, spec provider.Spec) (provider.State, error) {
	if len(spec.FrpsConfigTOML) == 0 {
		return provider.State{}, errors.New("localdocker: FrpsConfigTOML is required")
	}
	if spec.BindPort == 0 || spec.AdminPort == 0 {
		return provider.State{}, errors.New("localdocker: BindPort and AdminPort are required")
	}

	cfgPath, err := writeTempConfig(spec.FrpsConfigTOML)
	if err != nil {
		return provider.State{}, err
	}

	// Pull image if not present (silently swallow "already exists").
	rd, err := d.client.ImagePull(ctx, d.cfg.Image, image.PullOptions{})
	if err != nil {
		return provider.State{}, fmt.Errorf("image pull: %w", err)
	}
	_, _ = io.Copy(io.Discard, rd)
	_ = rd.Close()

	// Build port bindings: BindPort/tcp and AdminPort/tcp on 127.0.0.1.
	portSet := nat.PortSet{
		nat.Port(strconv.Itoa(spec.BindPort) + "/tcp"):  struct{}{},
		nat.Port(strconv.Itoa(spec.AdminPort) + "/tcp"): struct{}{},
	}
	portMap := nat.PortMap{
		nat.Port(strconv.Itoa(spec.BindPort) + "/tcp"): []nat.PortBinding{
			{HostIP: d.cfg.HostBindIP, HostPort: strconv.Itoa(spec.BindPort)},
		},
		nat.Port(strconv.Itoa(spec.AdminPort) + "/tcp"): []nat.PortBinding{
			{HostIP: d.cfg.HostBindIP, HostPort: strconv.Itoa(spec.AdminPort)},
		},
	}

	containerCfg := &container.Config{
		Image:        d.cfg.Image,
		ExposedPorts: portSet,
		Labels: map[string]string{
			"frp-operator.io/provider": d.cfg.Name,
			"frp-operator.io/exit":     spec.Name,
		},
	}
	if d.cfg.SkipHostPortPublishing {
		portMap = nat.PortMap{}
	}
	hostCfg := &container.HostConfig{
		PortBindings: portMap,
		Mounts: []mount.Mount{{
			Type:     mount.TypeBind,
			Source:   cfgPath,
			Target:   "/etc/frp/frps.toml",
			ReadOnly: true,
		}},
		AutoRemove: false,
	}
	netCfg := &network.NetworkingConfig{}
	if d.cfg.Network != "" {
		netCfg.EndpointsConfig = map[string]*network.EndpointSettings{
			d.cfg.Network: {},
		}
	}
	cname := sanitize(spec.Name)
	created, err := d.client.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, cname)
	if err != nil && errdefs.IsConflict(err) {
		// Stale container from a prior failed attempt; remove and retry once.
		_ = d.client.ContainerRemove(ctx, cname, container.RemoveOptions{Force: true})
		created, err = d.client.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, cname)
	}
	if err != nil {
		return provider.State{}, fmt.Errorf("container create: %w", err)
	}
	if err := d.client.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return provider.State{}, fmt.Errorf("container start: %w", err)
	}

	return d.Inspect(ctx, created.ID)
}

// Inspect implements provider.Provisioner.
func (d *LocalDocker) Inspect(ctx context.Context, providerID string) (provider.State, error) {
	resp, err := d.client.ContainerInspect(ctx, providerID)
	if err != nil {
		// Distinguish "no such container" from other errors.
		if errdefs.IsNotFound(err) {
			return provider.State{Phase: provider.PhaseGone}, fmt.Errorf("%s: %w", providerID, provider.ErrNotFound)
		}
		return provider.State{}, fmt.Errorf("inspect: %w", err)
	}
	state := provider.State{
		ProviderID: resp.ID,
		PublicIP:   d.cfg.HostBindIP,
	}
	if d.cfg.Network != "" {
		if ep, ok := resp.NetworkSettings.Networks[d.cfg.Network]; ok && ep != nil && ep.IPAddress != "" {
			state.PublicIP = ep.IPAddress
		}
	}
	switch {
	case resp.State.Running:
		state.Phase = provider.PhaseRunning
	case resp.State.Status == "created":
		state.Phase = provider.PhaseProvisioning
	default:
		state.Phase = provider.PhaseFailed
		state.Reason = resp.State.Error
	}
	return state, nil
}

// Destroy stops and removes the container. Idempotent: missing containers
// are not an error. A nil client (post-Close) is also a no-op.
func (d *LocalDocker) Destroy(ctx context.Context, providerID string) error {
	if d.client == nil {
		return nil
	}
	timeout := 5
	if err := d.client.ContainerStop(ctx, providerID, container.StopOptions{Timeout: &timeout}); err != nil {
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("stop: %w", err)
		}
	}
	if err := d.client.ContainerRemove(ctx, providerID, container.RemoveOptions{Force: true}); err != nil {
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("remove: %w", err)
		}
	}
	return nil
}

// writeTempConfig writes the rendered frps.toml to a per-container temp file
// the daemon can bind-mount. The caller is responsible for retaining the
// file for the container's lifetime; teardown via Destroy doesn't remove it
// (rely on tmpfs / OS cleanup).
func writeTempConfig(body []byte) (string, error) {
	dir, err := os.MkdirTemp("", "frp-operator-localdocker-")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	path := filepath.Join(dir, "frps.toml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

// sanitize converts an arbitrary name into a Docker-safe container name.
// Docker permits [a-zA-Z0-9_.-], starting with [a-zA-Z0-9].
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '.', c == '-':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 || (out[0] != '_' && !isAlnum(out[0])) {
		out = append([]byte{'x'}, out...)
	}
	return "frp-operator-" + string(out)
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// Compile-time check that *LocalDocker implements provider.Provisioner.
var _ provider.Provisioner = (*LocalDocker)(nil)

// Filter returns a Docker filter that matches all containers managed by
// this provisioner. Useful for cleanup scripts.
func (d *LocalDocker) Filter() filters.Args {
	f := filters.NewArgs()
	f.Add("label", "frp-operator.io/provider="+d.cfg.Name)
	return f
}
