// Package cf wraps the Cloudflare API surface cftui needs: tunnels, remote
// ingress configuration, and DNS records.
package cf

import "context"

type Tunnel struct {
	ID            string
	Name          string
	Status        string // healthy | degraded | down | inactive
	ConnectorN    int
	RouteN        int
	CreatedAt     string
	RemoteManaged bool
}

type Route struct {
	Hostname    string
	Path        string
	Service     string
	NoTLSVerify *bool
	OriginName  string
	HTTPHost    string
}

type Connector struct {
	ID       string
	Colo     string
	Version  string
	OriginIP string
	Uptime   string
}

// DNSChange describes one pending or applied DNS mutation; Action is one of
// create, update, delete, orphan.
type DNSChange struct {
	Hostname string
	Action   string
}

// Backend is the tunnel-management surface the UI depends on. RemoteBackend
// talks to the Cloudflare API using the origin certificate's embedded token;
// LocalBackend reads and writes ~/.cloudflared/config.yml.
type Backend interface {
	ListTunnels(ctx context.Context) ([]Tunnel, error)
	GetRoutes(ctx context.Context, tunnelID string) ([]Route, error)
	// PutRoutes replaces the full ingress list. Implementations must
	// read-modify-write and preserve the trailing catch-all rule.
	PutRoutes(ctx context.Context, tunnelID string, routes []Route) error
	SyncDNS(ctx context.Context, tunnelID string, routes []Route) ([]DNSChange, error)
	Health(ctx context.Context, tunnelID string) ([]Connector, error)
}
