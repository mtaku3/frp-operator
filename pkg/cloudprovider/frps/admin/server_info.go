package admin

import "context"

type ServerInfo struct {
	Version  string `json:"version"`
	BindPort int    `json:"bindPort"`
}

func (c *Client) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	var out ServerInfo
	if err := c.get(ctx, "/api/serverinfo", &out); err != nil {
		return nil, err
	}
	return &out, nil
}
