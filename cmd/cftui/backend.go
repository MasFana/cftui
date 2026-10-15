package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MasFana/cftui/internal/cf"
	"github.com/MasFana/cftui/internal/cred"
	"github.com/MasFana/cftui/internal/local"
)

// localBackend adapts a parsed cloudflared config.yml to cf.Backend, so the UI
// can list and stage routes with no network access.
type localBackend struct {
	file *local.File
	path string
	pipe *local.Pipeline
}

var _ cf.Backend = localBackend{}

// ListTunnels reports the ingress blocks present in the local config.
func (b localBackend) ListTunnels(context.Context) ([]cf.Tunnel, error) {
	names := b.file.TunnelNames()
	out := make([]cf.Tunnel, 0, len(names))
	for _, name := range names {
		t := cf.Tunnel{ID: name, Name: name, Status: "local"}
		rules, err := b.file.TunnelIngress(name)
		if err != nil {
			return nil, err
		}
		t.RouteN = len(rules)
		out = append(out, t)
	}
	return out, nil
}

// GetRoutes returns the ingress rules of one tunnel as cf.Route values.
func (b localBackend) GetRoutes(_ context.Context, tunnelID string) ([]cf.Route, error) {
	rules, err := b.file.TunnelIngress(tunnelID)
	if err != nil {
		return nil, err
	}
	out := make([]cf.Route, 0, len(rules))
	for _, r := range rules {
		out = append(out, cf.Route{
			Hostname:   r.Hostname,
			Path:       r.Path,
			Service:    r.Service,
			OriginName: r.OriginName,
			HTTPHost:   r.HTTPHost,
		})
	}
	return cf.EnsureCatchAllLast(out), nil
}

// PutRoutes writes the routes back to the local config, then validates the
// result with cloudflared and reloads it (PRD FR-19).
func (b localBackend) PutRoutes(ctx context.Context, tunnelID string, routes []cf.Route) error {
	rules := make([]local.Rule, 0, len(routes))
	for _, r := range cf.EnsureCatchAllLast(routes) {
		rules = append(rules, local.Rule{
			Hostname:   r.Hostname,
			Path:       r.Path,
			Service:    r.Service,
			OriginName: r.OriginName,
			HTTPHost:   r.HTTPHost,
		})
	}
	if err := b.file.SetTunnelIngress(tunnelID, rules); err != nil {
		return err
	}
	if b.pipe == nil {
		return b.file.Save()
	}
	_, err := b.pipe.Apply(ctx, b.file)
	return err
}

// SyncDNS reports no changes: the local config cannot create DNS records.
func (b localBackend) SyncDNS(context.Context, string, []cf.Route) ([]cf.DNSChange, error) {
	return nil, nil
}

// Health reports no connectors: connector state is a remote-only fact.
func (b localBackend) Health(context.Context, string) ([]cf.Connector, error) {
	return nil, nil
}

// CreateTunnel creates a tunnel by shelling out to the cloudflared CLI, which
// works without an API certificate. The returned RouteN is zero because a new
// tunnel has no ingress until routes are written.
func (b localBackend) CreateTunnel(ctx context.Context, name string) (cf.Tunnel, error) {
	if _, err := b.run(ctx, "tunnel", "create", name); err != nil {
		return cf.Tunnel{}, err
	}
	return cf.Tunnel{ID: name, Name: name, Status: "inactive"}, nil
}

// DeleteTunnel deletes a tunnel by shelling out to the cloudflared CLI. It
// refuses to run without an explicit name so a stray keypress cannot delete a
// tunnel by an empty id.
func (b localBackend) DeleteTunnel(ctx context.Context, tunnelID string) error {
	if tunnelID == "" {
		return errors.New("delete tunnel: tunnel id is empty")
	}
	_, err := b.run(ctx, "tunnel", "delete", "--force", tunnelID)
	return err
}

// TunnelToken fetches a tunnel's connector token by shelling out to the
// cloudflared CLI. The local backend has no API access, so the CLI resolves the
// tunnel name itself; a missing or deleted tunnel surfaces as an error.
func (b localBackend) TunnelToken(ctx context.Context, tunnelID string) (string, error) {
	if tunnelID == "" {
		return "", errors.New("tunnel token: tunnel id is empty")
	}
	out, err := b.run(ctx, "tunnel", "token", tunnelID)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("tunnel token %s: cloudflared returned an empty token", tunnelID)
	}
	return token, nil
}

// run executes a cloudflared subcommand through the configured verifier,
// defaulting to the real binary when no pipeline is present.
func (b localBackend) run(ctx context.Context, args ...string) ([]byte, error) {
	if b.pipe == nil {
		b.pipe = local.NewPipeline()
	}
	out, err := b.pipe.Verifier.Run(ctx, "cloudflared", args...)
	if err != nil {
		return out, fmt.Errorf("cloudflared %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// remoteBackend adapts the Cloudflare API to cf.Backend. It is only constructed
// from a valid origin certificate, so the API token never comes from the
// environment or a flag.
func remoteBackend(cert *cred.OriginCert) (cf.Backend, error) {
	if cert == nil {
		return nil, errors.New("remote backend: nil origin certificate")
	}
	api, err := cf.NewCloudflareAPI(cert.APIToken)
	if err != nil {
		return nil, fmt.Errorf("remote backend: %w", err)
	}
	return cf.NewRemoteBackend(cert.AccountID, cert.ZoneID, api)
}
