package cf

import (
	"context"
	"errors"
)

// RemoteBackend manages remotely-managed tunnels through the Cloudflare API
// using the token embedded in the cloudflared origin certificate.
type RemoteBackend struct {
	AccountID string
	ZoneID    string
	api       API
}

// API is the slice of cloudflare-go that cf depends on, kept behind an
// interface so tests run without network access or a live credential.
type API interface {
	ListTunnels(ctx context.Context, accountID string) ([]Tunnel, error)
	CreateTunnel(ctx context.Context, accountID, name string) (Tunnel, error)
	DeleteTunnel(ctx context.Context, accountID, tunnelID string) error
	GetTunnelToken(ctx context.Context, accountID, tunnelID string) (string, error)
	GetTunnelConfig(ctx context.Context, accountID, tunnelID string) ([]Route, error)
	PutTunnelConfig(ctx context.Context, accountID, tunnelID string, routes []Route) error
	ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error)
	Connections(ctx context.Context, accountID, tunnelID string) ([]Connector, error)
}

func NewRemoteBackend(accountID, zoneID string, api API) (*RemoteBackend, error) {
	if accountID == "" {
		return nil, errors.New("cf: account id is required (set by the origin certificate)")
	}
	if zoneID == "" {
		return nil, errors.New("cf: zone id is required (set by the origin certificate)")
	}
	if api == nil {
		return nil, errors.New("cf: api client is required")
	}
	return &RemoteBackend{AccountID: accountID, ZoneID: zoneID, api: api}, nil
}

func (b *RemoteBackend) ListTunnels(ctx context.Context) ([]Tunnel, error) {
	return b.api.ListTunnels(ctx, b.AccountID)
}

// CreateTunnel creates a tunnel through the API. This is the path the UI takes
// whenever an origin certificate is available, so creating a tunnel needs no
// local cloudflared invocation.
func (b *RemoteBackend) CreateTunnel(ctx context.Context, name string) (Tunnel, error) {
	return b.api.CreateTunnel(ctx, b.AccountID, name)
}

// DeleteTunnel removes a tunnel through the API.
func (b *RemoteBackend) DeleteTunnel(ctx context.Context, tunnelID string) error {
	return b.api.DeleteTunnel(ctx, b.AccountID, tunnelID)
}

// TunnelToken fetches the connector token for a tunnel through the API. It is
// keyed by tunnel ID, so it works for any tunnel the account owns without
// depending on the local cloudflared name resolution.
func (b *RemoteBackend) TunnelToken(ctx context.Context, tunnelID string) (string, error) {
	return b.api.GetTunnelToken(ctx, b.AccountID, tunnelID)
}

func (b *RemoteBackend) GetRoutes(ctx context.Context, tunnelID string) ([]Route, error) {
	return b.api.GetTunnelConfig(ctx, b.AccountID, tunnelID)
}

func (b *RemoteBackend) PutRoutes(ctx context.Context, tunnelID string, routes []Route) error {
	return b.api.PutTunnelConfig(ctx, b.AccountID, tunnelID, EnsureCatchAllLast(routes))
}

// SyncDNS plans the DNS changes the tunnel's routes require and returns them
// for confirmation. Reading the zone is the API's job; deciding what to change
// is PlanDNS's, so both backends agree on the outcome.
func (b *RemoteBackend) SyncDNS(ctx context.Context, tunnelID string, routes []Route) ([]DNSChange, error) {
	existing, err := b.api.ListDNSRecords(ctx, b.ZoneID)
	if err != nil {
		return nil, err
	}
	return DNSChanges(PlanDNS(tunnelID, TunnelCNAME(tunnelID), routes, existing)), nil
}

func (b *RemoteBackend) Health(ctx context.Context, tunnelID string) ([]Connector, error) {
	return b.api.Connections(ctx, b.AccountID, tunnelID)
}
