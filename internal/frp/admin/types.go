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
