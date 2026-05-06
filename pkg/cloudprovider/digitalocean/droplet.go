package digitalocean

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalocean/godo"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
)

const (
	// ManagedTag is added to every droplet provisioned by this operator.
	ManagedTag = "frp-operator-managed"

	providerIDPrefix = "do://"
)

// dropletAPI is the subset of godo.DropletsService we use; declared as
// an interface so tests can stub it.
type dropletAPI interface {
	Create(ctx context.Context, req *godo.DropletCreateRequest) (*godo.Droplet, *godo.Response, error)
	Get(ctx context.Context, id int) (*godo.Droplet, *godo.Response, error)
	Delete(ctx context.Context, id int) (*godo.Response, error)
	ListByTag(ctx context.Context, tag string, opt *godo.ListOptions) ([]godo.Droplet, *godo.Response, error)
	ListByName(ctx context.Context, name string, opt *godo.ListOptions) ([]godo.Droplet, *godo.Response, error)
}

// sanitizeDropletName turns an arbitrary claim name into a DO-acceptable
// hostname.
func sanitizeDropletName(s string) string {
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

// dropletNameFor produces the deterministic droplet name for a claim.
func dropletNameFor(claim *v1alpha1.ExitClaim) string {
	return "frp-operator-" + sanitizeDropletName(claim.Name)
}

// findByName looks up a droplet by deterministic name (idempotency).
func findByName(ctx context.Context, api dropletAPI, name string) (*godo.Droplet, error) {
	droplets, _, err := api.ListByName(ctx, name, nil)
	if err != nil {
		return nil, fmt.Errorf("list by name: %w", err)
	}
	for i := range droplets {
		if droplets[i].Name == name {
			return &droplets[i], nil
		}
	}
	return nil, nil
}

// listManaged returns all droplets tagged ManagedTag.
func listManaged(ctx context.Context, api dropletAPI) ([]godo.Droplet, error) {
	out, _, err := api.ListByTag(ctx, ManagedTag, nil)
	if err != nil {
		return nil, fmt.Errorf("list by tag: %w", err)
	}
	return out, nil
}

// dropletGet wraps godo Get and surfaces ExitNotFoundError on 404.
func dropletGet(ctx context.Context, api dropletAPI, providerID string) (*godo.Droplet, error) {
	id, err := parseProviderID(providerID)
	if err != nil {
		return nil, err
	}
	d, resp, err := api.Get(ctx, id)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return nil, cloudprovider.NewExitNotFoundError(providerID)
		}
		return nil, fmt.Errorf("get droplet: %w", err)
	}
	return d, nil
}

// dropletDelete deletes by providerID, treating 404 as ExitNotFoundError.
func dropletDelete(ctx context.Context, api dropletAPI, providerID string) error {
	id, err := parseProviderID(providerID)
	if err != nil {
		return err
	}
	resp, err := api.Delete(ctx, id)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return cloudprovider.NewExitNotFoundError(providerID)
		}
		return fmt.Errorf("delete droplet: %w", err)
	}
	return nil
}

// parseProviderID extracts the integer droplet ID from a "do://<int>" string.
func parseProviderID(providerID string) (int, error) {
	s := strings.TrimPrefix(providerID, providerIDPrefix)
	id, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("parse providerID %q: %w", providerID, err)
	}
	return id, nil
}

// providerIDFor formats a droplet ID as a providerID string.
func providerIDFor(id int) string {
	return providerIDPrefix + strconv.Itoa(id)
}

// publicIPv4 picks the first public IPv4 address from a droplet's
// network listing.
func publicIPv4(d *godo.Droplet) string {
	if d == nil || d.Networks == nil {
		return ""
	}
	for _, net := range d.Networks.V4 {
		if net.Type == "public" {
			return net.IPAddress
		}
	}
	return ""
}
