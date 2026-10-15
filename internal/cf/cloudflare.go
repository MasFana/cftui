package cf

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	cfapi "github.com/cloudflare/cloudflare-go"
)

// CloudflareAPI is the live API implementation backed by
// github.com/cloudflare/cloudflare-go.
//
// The client is built from the API token embedded in the local cloudflared
// origin certificate, and the account and zone are passed as explicit string
// arguments at each call site. The token is held only inside the unexported
// cloudflare-go client so it can never appear in an error, a log line, or a
// printed struct.
type CloudflareAPI struct {
	api *cfapi.API
}

// NewCloudflareAPI builds a client from the API token embedded in the
// cloudflared origin certificate.
func NewCloudflareAPI(apiToken string) (*CloudflareAPI, error) {
	if apiToken == "" {
		return nil, fmt.Errorf("cloudflare: api token is empty")
	}
	client, err := cfapi.NewWithAPIToken(apiToken)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: build client: %w", err)
	}
	return &CloudflareAPI{api: client}, nil
}

// ListTunnels lists the tunnels belonging to the account.
func (c *CloudflareAPI) ListTunnels(ctx context.Context, accountID string) ([]Tunnel, error) {
	if accountID == "" {
		return nil, fmt.Errorf("list tunnels: account id is empty")
	}
	raw, _, err := c.api.ListTunnels(ctx, cfapi.AccountIdentifier(accountID), cfapi.TunnelListParams{})
	if err != nil {
		return nil, fmt.Errorf("list tunnels: %w", err)
	}
	out := make([]Tunnel, 0, len(raw))
	for _, t := range raw {
		out = append(out, Tunnel{
			ID:            t.ID,
			Name:          t.Name,
			Status:        t.Status,
			ConnectorN:    len(t.Connections),
			RemoteManaged: t.RemoteConfig,
			CreatedAt:     formatTime(t.CreatedAt),
		})
	}
	return out, nil
}

// TunnelToken fetches the connector token that lets another host run the
// tunnel. It is a live credential, so the caller must treat it as a secret.
func (c *CloudflareAPI) GetTunnelToken(ctx context.Context, accountID, tunnelID string) (string, error) {
	if accountID == "" {
		return "", fmt.Errorf("tunnel token: account id is empty")
	}
	if tunnelID == "" {
		return "", fmt.Errorf("tunnel token: tunnel id is empty")
	}
	token, err := c.api.GetTunnelToken(ctx, cfapi.AccountIdentifier(accountID), tunnelID)
	if err != nil {
		return "", fmt.Errorf("tunnel token %s: %w", tunnelID, err)
	}
	if token == "" {
		return "", fmt.Errorf("tunnel token %s: empty token from api", tunnelID)
	}
	return token, nil
}

// CreateTunnel creates a tunnel in the account. The 32-byte secret is
// generated here: cloudflared stores the same value in the tunnel's credentials
// file, and the API rejects a create without one.
func (c *CloudflareAPI) CreateTunnel(ctx context.Context, accountID, name string) (Tunnel, error) {
	if accountID == "" {
		return Tunnel{}, fmt.Errorf("create tunnel: account id is empty")
	}
	if name == "" {
		return Tunnel{}, fmt.Errorf("create tunnel: name is empty")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return Tunnel{}, fmt.Errorf("create tunnel: generate secret: %w", err)
	}
	t, err := c.api.CreateTunnel(ctx, cfapi.AccountIdentifier(accountID), cfapi.TunnelCreateParams{
		Name:      name,
		Secret:    base64.StdEncoding.EncodeToString(secret),
		ConfigSrc: "cloudflare",
	})
	if err != nil {
		return Tunnel{}, fmt.Errorf("create tunnel %s: %w", name, err)
	}
	return Tunnel{ID: t.ID, Name: t.Name, Status: t.Status, CreatedAt: formatTime(t.CreatedAt)}, nil
}

// DeleteTunnel removes a tunnel from the account.
func (c *CloudflareAPI) DeleteTunnel(ctx context.Context, accountID, tunnelID string) error {
	if accountID == "" || tunnelID == "" {
		return fmt.Errorf("delete tunnel: account id and tunnel id are required")
	}
	if err := c.api.DeleteTunnel(ctx, cfapi.AccountIdentifier(accountID), tunnelID); err != nil {
		return fmt.Errorf("delete tunnel %s: %w", tunnelID, err)
	}
	return nil
}

// GetTunnelConfig returns the remote ingress rules of one tunnel.
func (c *CloudflareAPI) GetTunnelConfig(ctx context.Context, accountID, tunnelID string) ([]Route, error) {
	if accountID == "" || tunnelID == "" {
		return nil, fmt.Errorf("get tunnel config: account id and tunnel id are required")
	}
	res, err := c.api.GetTunnelConfiguration(ctx, cfapi.AccountIdentifier(accountID), tunnelID)
	if err != nil {
		return nil, fmt.Errorf("get tunnel config %s: %w", tunnelID, err)
	}
	return routesFromIngress(res.Config.Ingress), nil
}

// PutTunnelConfig replaces the remote ingress list. The order given is the
// order sent: the caller has already applied EnsureCatchAllLast, so this
// method must not reorder or append anything.
func (c *CloudflareAPI) PutTunnelConfig(ctx context.Context, accountID, tunnelID string, routes []Route) error {
	if accountID == "" || tunnelID == "" {
		return fmt.Errorf("put tunnel config: account id and tunnel id are required")
	}
	params := cfapi.TunnelConfigurationParams{
		TunnelID: tunnelID,
		Config: cfapi.TunnelConfiguration{
			Ingress: ingressFromRoutes(routes),
		},
	}
	if _, err := c.api.UpdateTunnelConfiguration(ctx, cfapi.AccountIdentifier(accountID), params); err != nil {
		return fmt.Errorf("put tunnel config %s: %w", tunnelID, err)
	}
	return nil
}

// ListDNSRecords lists the DNS records of a zone in the shape PlanDNS needs.
func (c *CloudflareAPI) ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	if zoneID == "" {
		return nil, fmt.Errorf("list dns records: zone id is empty")
	}
	raw, _, err := c.api.ListDNSRecords(ctx, cfapi.ZoneIdentifier(zoneID), cfapi.ListDNSRecordsParams{})
	if err != nil {
		return nil, fmt.Errorf("list dns records: %w", err)
	}
	out := make([]DNSRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, DNSRecord{
			Name:         r.Name,
			Type:         r.Type,
			Content:      r.Content,
			Proxied:      r.Proxied != nil && *r.Proxied,
			OwnerTunnel:  ownerFromCNAME(r.Content),
			OwnerComment: r.Comment,
		})
	}
	return out, nil
}

// ownerFromCNAME recovers the tunnel id a *.cfargotunnel.com CNAME points at,
// so a later sync can tell cftui's records from records a human made by hand.
func ownerFromCNAME(content string) string {
	const suffix = ".cfargotunnel.com"
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(content)), suffix)
	if name == strings.ToLower(strings.TrimSpace(content)) {
		return ""
	}
	return name
}

// Connections lists the live connectors of one tunnel.
func (c *CloudflareAPI) Connections(ctx context.Context, accountID, tunnelID string) ([]Connector, error) {
	if accountID == "" || tunnelID == "" {
		return nil, fmt.Errorf("connections: account id and tunnel id are required")
	}
	raw, err := c.api.ListTunnelConnections(ctx, cfapi.AccountIdentifier(accountID), tunnelID)
	if err != nil {
		return nil, fmt.Errorf("connections %s: %w", tunnelID, err)
	}
	out := make([]Connector, 0, len(raw))
	for _, conn := range raw {
		for _, tc := range conn.Connections {
			out = append(out, Connector{
				ID:       conn.ID,
				Version:  conn.Version,
				Colo:     tc.ColoName,
				OriginIP: tc.OriginIP,
				Uptime:   formatTime(conn.RunAt),
			})
		}
	}
	return out, nil
}

// routesFromIngress converts remote ingress rules into cf.Route values.
//
// The origin certificate's originRequest carries only httpHostHeader in this
// client version, so OriginName is left empty when reading remote config; it is
// still written locally, where cloudflared supports it.
func routesFromIngress(in []cfapi.UnvalidatedIngressRule) []Route {
	out := make([]Route, 0, len(in))
	for _, r := range in {
		rt := Route{Hostname: r.Hostname, Path: r.Path, Service: r.Service}
		if r.OriginRequest != nil {
			if r.OriginRequest.HTTPHostHeader != nil {
				rt.HTTPHost = *r.OriginRequest.HTTPHostHeader
			}
		}
		out = append(out, rt)
	}
	return out
}

func ingressFromRoutes(routes []Route) []cfapi.UnvalidatedIngressRule {
	out := make([]cfapi.UnvalidatedIngressRule, 0, len(routes))
	for _, r := range routes {
		rule := cfapi.UnvalidatedIngressRule{
			Hostname: r.Hostname,
			Path:     r.Path,
			Service:  r.Service,
		}
		if r.HTTPHost != "" {
			or := &cfapi.OriginRequestConfig{}
			host := r.HTTPHost
			or.HTTPHostHeader = &host
			rule.OriginRequest = or
		}
		out = append(out, rule)
	}
	return out
}

// formatTime renders an optional timestamp as RFC3339, or "" when absent.
func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// TunnelCNAME is the CNAME target Cloudflare serves a tunnel on.
func TunnelCNAME(tunnelID string) string {
	return tunnelID + ".cfargotunnel.com"
}

var _ API = (*CloudflareAPI)(nil)
