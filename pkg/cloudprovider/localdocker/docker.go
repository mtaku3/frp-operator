package localdocker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/frps"
	ldv1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/localdocker/v1alpha1"
)

// providerLabel is set on every container managed by this package.
const (
	providerLabel = "frp-operator.io/provider"
	exitNameLabel = "frp-operator.io/exit"
	providerName  = "local-docker"
	hostBindIP    = "127.0.0.1"

	providerIDPrefix = "localdocker://"
)

type dockerOps struct {
	cli *client.Client
}

func newDockerOps() (*dockerOps, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &dockerOps{cli: cli}, nil
}

// containerName produces the deterministic container name used both
// by Create (idempotency) and Get/Delete.
func containerName(claim *v1alpha1.ExitClaim) string {
	return "frp-operator-" + sanitizeName(claim.Name)
}

// sanitizeName returns a Docker-safe name fragment.
func sanitizeName(s string) string {
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
	if len(out) == 0 {
		return "x"
	}
	if !isAlnum(out[0]) && out[0] != '_' {
		out = append([]byte{'x'}, out...)
	}
	return string(out)
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// imageRef substitutes the frps version into pc.Spec.DefaultImage.
// Default template is "fatedier/frps:%s".
func imageRef(pc *ldv1alpha1.LocalDockerProviderClass, version string) string {
	tpl := pc.Spec.DefaultImage
	if tpl == "" {
		tpl = "fatedier/frps:%s"
	}
	if strings.Contains(tpl, "%s") {
		return fmt.Sprintf(tpl, version)
	}
	return tpl
}

// writeConfig writes frps.toml under pc.Spec.ConfigHostMountPath/<name>/.
// Returns the host path (file) and target directory the daemon will read
// (the bind-mount target is the file path mapped to /etc/frp/frps.toml).
func writeConfig(pc *ldv1alpha1.LocalDockerProviderClass, claim *v1alpha1.ExitClaim, body string) (string, error) {
	root := pc.Spec.ConfigHostMountPath
	if root == "" {
		root = "/tmp/frp-operator-shared"
	}
	dir := filepath.Join(root, sanitizeName(claim.Name))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir config: %w", err)
	}
	path := filepath.Join(dir, "frps.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("write frps.toml: %w", err)
	}
	return path, nil
}

// ensureContainer is the create path. Idempotent: if a container with the
// deterministic name already exists, it returns its ID without re-creating.
// Returns the providerID (localdocker://<container ID>).
func (d *dockerOps) ensureContainer(ctx context.Context, claim *v1alpha1.ExitClaim, pc *ldv1alpha1.LocalDockerProviderClass, authToken string) (string, error) {
	name := containerName(claim)

	// 1. Idempotency: look up by name.
	if id, ok, err := d.findByName(ctx, name); err != nil {
		return "", err
	} else if ok {
		return providerIDPrefix + id, nil
	}

	// 2. Render frps.toml.
	body, err := frps.RenderConfig(claim.Spec.Frps, authToken)
	if err != nil {
		return "", fmt.Errorf("render frps config: %w", err)
	}
	cfgPath, err := writeConfig(pc, claim, body)
	if err != nil {
		return "", err
	}

	// 3. Pull image (best-effort).
	imgRef := imageRef(pc, claim.Spec.Frps.Version)
	rd, err := d.cli.ImagePull(ctx, imgRef, image.PullOptions{})
	if err != nil {
		return "", fmt.Errorf("image pull: %w", err)
	}
	_, _ = io.Copy(io.Discard, rd)
	_ = rd.Close()

	// 4. Build port spec.
	bindPort := claim.Spec.Frps.BindPort
	if bindPort == 0 {
		bindPort = 7000
	}
	adminPort := claim.Spec.Frps.AdminPort
	if adminPort == 0 {
		adminPort = 7400
	}
	portSet := nat.PortSet{
		nat.Port(strconv.Itoa(int(bindPort)) + "/tcp"):  struct{}{},
		nat.Port(strconv.Itoa(int(adminPort)) + "/tcp"): struct{}{},
	}
	portMap := nat.PortMap{}
	if !pc.Spec.SkipHostPortPublishing {
		portMap = nat.PortMap{
			nat.Port(strconv.Itoa(int(bindPort)) + "/tcp"): []nat.PortBinding{
				{HostIP: hostBindIP, HostPort: strconv.Itoa(int(bindPort))},
			},
			nat.Port(strconv.Itoa(int(adminPort)) + "/tcp"): []nat.PortBinding{
				{HostIP: hostBindIP, HostPort: strconv.Itoa(int(adminPort))},
			},
		}
	}

	containerCfg := &container.Config{
		Image:        imgRef,
		ExposedPorts: portSet,
		Labels: map[string]string{
			providerLabel: providerName,
			exitNameLabel: claim.Name,
		},
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
	if pc.Spec.Network != "" {
		netCfg.EndpointsConfig = map[string]*network.EndpointSettings{
			pc.Spec.Network: {},
		}
	}

	created, err := d.cli.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, name)
	if err != nil && errdefs.IsConflict(err) {
		// Stale container from a prior failed attempt; remove and retry once.
		_ = d.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
		created, err = d.cli.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, name)
	}
	if err != nil {
		return "", fmt.Errorf("container create: %w", err)
	}
	if err := d.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("container start: %w", err)
	}
	return providerIDPrefix + created.ID, nil
}

// findByName returns the (id, found, err) tuple for a container by name.
func (d *dockerOps) findByName(ctx context.Context, name string) (string, bool, error) {
	f := filters.NewArgs()
	f.Add("name", "^/"+name+"$")
	items, err := d.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return "", false, fmt.Errorf("list containers: %w", err)
	}
	for _, it := range items {
		for _, n := range it.Names {
			if n == "/"+name {
				return it.ID, true, nil
			}
		}
	}
	return "", false, nil
}

// inspect returns a hydrated ExitClaim built from a running container.
func (d *dockerOps) inspect(ctx context.Context, providerID string) (*v1alpha1.ExitClaim, error) {
	id := strings.TrimPrefix(providerID, providerIDPrefix)
	resp, err := d.cli.ContainerInspect(ctx, id)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, cloudprovider.NewExitNotFoundError(providerID)
		}
		return nil, fmt.Errorf("inspect: %w", err)
	}
	out := &v1alpha1.ExitClaim{}
	if name, ok := resp.Config.Labels[exitNameLabel]; ok {
		out.Name = name
	}
	out.Status.ProviderID = providerID
	out.Status.ExitName = strings.TrimPrefix(resp.Name, "/")
	out.Status.PublicIP = hostBindIP
	if resp.NetworkSettings != nil {
		for _, ep := range resp.NetworkSettings.Networks {
			if ep != nil && ep.IPAddress != "" {
				out.Status.PublicIP = ep.IPAddress
				break
			}
		}
	}
	if resp.Image != "" {
		out.Status.ImageID = resp.Image
	} else if resp.Config != nil {
		out.Status.ImageID = resp.Config.Image
	}
	return out, nil
}

// remove stops + removes the container. Returns ExitNotFoundError if the
// container is already gone.
func (d *dockerOps) remove(ctx context.Context, providerID string) error {
	id := strings.TrimPrefix(providerID, providerIDPrefix)
	timeout := 5
	if err := d.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
		if errdefs.IsNotFound(err) {
			return cloudprovider.NewExitNotFoundError(providerID)
		}
		// Stop failures aren't fatal; try to remove anyway.
	}
	if err := d.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		if errdefs.IsNotFound(err) {
			return cloudprovider.NewExitNotFoundError(providerID)
		}
		return fmt.Errorf("remove: %w", err)
	}
	return nil
}

// listManaged enumerates all containers labeled by this provider.
func (d *dockerOps) listManaged(ctx context.Context) ([]*v1alpha1.ExitClaim, error) {
	f := filters.NewArgs()
	f.Add("label", providerLabel+"="+providerName)
	items, err := d.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]*v1alpha1.ExitClaim, 0, len(items))
	for _, it := range items {
		claim := &v1alpha1.ExitClaim{}
		if name, ok := it.Labels[exitNameLabel]; ok {
			claim.Name = name
		}
		claim.Status.ProviderID = providerIDPrefix + it.ID
		out = append(out, claim)
	}
	return out, nil
}
