package cf

import (
	"context"
	"testing"
)

func TestEnsureCatchAllLastAppendsWhenAbsent(t *testing.T) {
	got := EnsureCatchAllLast([]Route{{Hostname: "a.example.com", Service: "http://localhost:1"}})
	if len(got) != 2 {
		t.Fatalf("want 2 routes, got %d: %+v", len(got), got)
	}
	if !IsCatchAll(got[len(got)-1]) || got[len(got)-1].Service != DefaultCatchAll {
		t.Fatalf("catch-all must be appended last with default service: %+v", got)
	}
}

func TestEnsureCatchAllLastMovesExistingToEnd(t *testing.T) {
	in := []Route{
		{Service: "http_status:404"},
		{Hostname: "a.example.com", Service: "http://localhost:1"},
		{Hostname: "b.example.com", Service: "http://localhost:2"},
	}
	got := EnsureCatchAllLast(in)
	if len(got) != 3 {
		t.Fatalf("want 3 routes, got %d", len(got))
	}
	wantOrder := []string{"a.example.com", "b.example.com", ""}
	for i, w := range wantOrder {
		if got[i].Hostname != w {
			t.Fatalf("index %d: got hostname %q want %q", i, got[i].Hostname, w)
		}
	}
}

func TestEnsureCatchAllLastDeduplicates(t *testing.T) {
	in := []Route{
		{Hostname: "a.example.com", Service: "http://localhost:1"},
		{Service: "http_status:503"},
		{Service: "http_status:404"},
	}
	got := EnsureCatchAllLast(in)
	catchalls := 0
	for _, r := range got {
		if IsCatchAll(r) {
			catchalls++
		}
	}
	if catchalls != 1 {
		t.Fatalf("want exactly 1 catch-all, got %d: %+v", catchalls, got)
	}
	if got[len(got)-1].Service != "http_status:503" {
		t.Fatalf("first-seen catch-all must be preserved: %+v", got)
	}
}

func TestEnsureCatchAllLastPreservesHostnameOrder(t *testing.T) {
	in := []Route{
		{Hostname: "z.example.com", Service: "http://localhost:9"},
		{Service: "http_status:404"},
		{Hostname: "a.example.com", Service: "http://localhost:1"},
	}
	got := EnsureCatchAllLast(in)
	if got[0].Hostname != "z.example.com" || got[1].Hostname != "a.example.com" {
		t.Fatalf("user ordering must be preserved: %+v", got)
	}
}

func TestNewRemoteBackendRejectsIncompleteCredential(t *testing.T) {
	if _, err := NewRemoteBackend("", "zone", fakeAPI{}); err == nil {
		t.Fatal("empty account id must be rejected")
	}
	if _, err := NewRemoteBackend("acct", "", fakeAPI{}); err == nil {
		t.Fatal("empty zone id must be rejected")
	}
	if _, err := NewRemoteBackend("acct", "zone", nil); err == nil {
		t.Fatal("nil api must be rejected")
	}
	b, err := NewRemoteBackend("acct", "zone", fakeAPI{})
	if err != nil {
		t.Fatalf("valid credential must be accepted: %v", err)
	}
	if b.AccountID != "acct" || b.ZoneID != "zone" {
		t.Fatalf("scope not recorded: %+v", b)
	}
}

type fakeAPI struct {
	put []Route
}

func (fakeAPI) ListTunnels(ctx context.Context, accountID string) ([]Tunnel, error) {
	return nil, nil
}
func (fakeAPI) CreateTunnel(ctx context.Context, accountID, name string) (Tunnel, error) {
	return Tunnel{}, nil
}
func (fakeAPI) DeleteTunnel(ctx context.Context, accountID, tunnelID string) error {
	return nil
}
func (fakeAPI) GetTunnelToken(ctx context.Context, accountID, tunnelID string) (string, error) {
	return "", nil
}
func (fakeAPI) GetTunnelConfig(ctx context.Context, accountID, tunnelID string) ([]Route, error) {
	return nil, nil
}
func (fakeAPI) PutTunnelConfig(ctx context.Context, accountID, tunnelID string, routes []Route) error {
	return nil
}
func (fakeAPI) ListDNSRecords(context.Context, string) ([]DNSRecord, error) {
	return nil, nil
}
func (fakeAPI) Connections(ctx context.Context, accountID, tunnelID string) ([]Connector, error) {
	return nil, nil
}
