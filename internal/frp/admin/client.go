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
	defer func() { _ = resp.Body.Close() }()
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
	resp, err := c.do(ctx, http.MethodPut, "/api/config", bytes.NewReader(configBody))
	if err != nil {
		return fmt.Errorf("put config: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body) // drain to allow keep-alive reuse
	_ = resp.Body.Close()

	resp, err = c.do(ctx, http.MethodGet, "/api/reload", nil)
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	return nil
}

// ListProxies calls GET /api/proxy/<proxyType> ("tcp" or "udp"). Returns
// the raw Proxy list; status interpretation is the caller's job.
func (c *Client) ListProxies(ctx context.Context, proxyType string) ([]Proxy, error) {
	resp, err := c.do(ctx, http.MethodGet, "/api/proxy/"+proxyType, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
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
