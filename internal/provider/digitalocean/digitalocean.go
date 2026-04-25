// Package digitalocean is the production Provisioner for DigitalOcean.
// It creates a Droplet per ExitServer with the operator's cloud-init user
// data, polls droplet status for Inspect, and deletes on Destroy. Token
// is per-call from spec.Credentials (loaded by the controller from a
// Secret referenced by ExitServer.spec.credentialsRef).
package digitalocean

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/digitalocean/godo"

	"github.com/mtaku3/frp-operator/internal/provider"
)

// Config tunes the DigitalOcean provisioner. Token is required for runtime
// API access; per-Spec credentials override it. BaseURL is for testing.
type Config struct {
	// Token is the DO API token. Spec.Credentials overrides it per-call.
	Token string
	// BaseURL overrides the godo default (https://api.digitalocean.com/);
	// tests point this at an httptest.Server.
	BaseURL string
}

// DigitalOcean implements provider.Provisioner.
type DigitalOcean struct {
	cfg Config
}

// New constructs a DigitalOcean provisioner. The provided Token is used
// when Spec.Credentials is empty.
func New(cfg Config) (*DigitalOcean, error) {
	return &DigitalOcean{cfg: cfg}, nil
}

// Name implements provider.Provisioner.
func (d *DigitalOcean) Name() string { return "digitalocean" }

// client builds a per-call godo.Client. Token comes from Spec.Credentials
// if set, falling back to cfg.Token.
func (d *DigitalOcean) client(spec provider.Spec) (*godo.Client, error) {
	token := string(spec.Credentials)
	if token == "" {
		token = d.cfg.Token
	}
	if token == "" {
		return nil, errors.New("digitalocean: no API token in Spec.Credentials or Config.Token")
	}
	c := godo.NewFromToken(token)
	if d.cfg.BaseURL != "" {
		base := d.cfg.BaseURL
		if base[len(base)-1] != '/' {
			base += "/"
		}
		u, err := url.Parse(base)
		if err != nil {
			return nil, fmt.Errorf("parse BaseURL: %w", err)
		}
		c.BaseURL = u
	}
	return c, nil
}

// clientFromID is for Inspect/Destroy where Spec is not available. Falls
// back to cfg.Token only — operators MUST set the token at construction
// time if they want post-restart Inspect/Destroy to work.
func (d *DigitalOcean) clientFromID() (*godo.Client, error) {
	return d.client(provider.Spec{})
}

// Create implements provider.Provisioner.
func (d *DigitalOcean) Create(ctx context.Context, spec provider.Spec) (provider.State, error) {
	c, err := d.client(spec)
	if err != nil {
		return provider.State{}, err
	}
	req := &godo.DropletCreateRequest{
		Name:     sanitizeName(spec.Name),
		Region:   spec.Region,
		Size:     spec.Size,
		Image:    godo.DropletCreateImage{Slug: "ubuntu-24-04-x64"},
		UserData: string(spec.CloudInitUserData),
	}
	droplet, _, err := c.Droplets.Create(ctx, req)
	if err != nil {
		return provider.State{}, fmt.Errorf("create droplet: %w", err)
	}
	return mapDroplet(droplet), nil
}

// Inspect implements provider.Provisioner.
func (d *DigitalOcean) Inspect(ctx context.Context, providerID string) (provider.State, error) {
	c, err := d.clientFromID()
	if err != nil {
		return provider.State{}, err
	}
	id, err := strconv.Atoi(providerID)
	if err != nil {
		return provider.State{}, fmt.Errorf("parse providerID %q: %w", providerID, err)
	}
	droplet, resp, err := c.Droplets.Get(ctx, id)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return provider.State{Phase: provider.PhaseGone}, fmt.Errorf("droplet %d: %w", id, provider.ErrNotFound)
		}
		return provider.State{}, fmt.Errorf("inspect droplet: %w", err)
	}
	return mapDroplet(droplet), nil
}

// Destroy implements provider.Provisioner.
func (d *DigitalOcean) Destroy(ctx context.Context, providerID string) error {
	c, err := d.clientFromID()
	if err != nil {
		return err
	}
	id, err := strconv.Atoi(providerID)
	if err != nil {
		return fmt.Errorf("parse providerID %q: %w", providerID, err)
	}
	resp, err := c.Droplets.Delete(ctx, id)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			// Already gone; idempotent.
			return nil
		}
		return fmt.Errorf("destroy droplet: %w", err)
	}
	return nil
}

// mapDroplet converts a godo.Droplet to a provider.State.
func mapDroplet(d *godo.Droplet) provider.State {
	if d == nil {
		return provider.State{Phase: provider.PhaseGone}
	}
	st := provider.State{
		ProviderID: strconv.Itoa(d.ID),
	}
	// Public IPv4 is the first network of type "public".
	if d.Networks != nil {
		for _, net := range d.Networks.V4 {
			if net.Type == "public" {
				st.PublicIP = net.IPAddress
				break
			}
		}
	}
	switch d.Status {
	case "active":
		st.Phase = provider.PhaseRunning
	case "new":
		st.Phase = provider.PhaseProvisioning
	case "off", "archive":
		st.Phase = provider.PhaseFailed
		st.Reason = "droplet status " + d.Status
	default:
		st.Phase = provider.PhaseProvisioning
	}
	return st
}

// sanitizeName turns "ns__name" into a DO-acceptable droplet name.
// DO accepts hostnames so [a-zA-Z0-9._-].
func sanitizeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' {
			out = append(out, c)
		} else {
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "frp-operator-exit"
	}
	return string(out)
}
