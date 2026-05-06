package admin

import "context"

type Proxy struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	RemotePort int    `json:"remotePort,omitempty"`
}

func (c *Client) ListProxies(ctx context.Context) ([]Proxy, error) {
	var out struct {
		Proxies []Proxy `json:"proxies"`
	}
	if err := c.get(ctx, "/api/proxy/tcp", &out); err != nil {
		return nil, err
	}
	return out.Proxies, nil
}
