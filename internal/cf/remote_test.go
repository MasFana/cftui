package cf

import (
	"context"
	"errors"
	"testing"
)

type recordingAPI struct {
	putRoutes  []Route
	putErr     error
	dnsRecords []DNSRecord
	created    []string
	deleted    []string
	token      string
	createErr  error
	deleteErr  error
	tokenErr   error
}

func (r *recordingAPI) ListTunnels(context.Context, string) ([]Tunnel, error) { return nil, nil }
func (r *recordingAPI) CreateTunnel(_ context.Context, _, name string) (Tunnel, error) {
	r.created = append(r.created, name)
	if r.createErr != nil {
		return Tunnel{}, r.createErr
	}
	return Tunnel{ID: "id-" + name, Name: name}, nil
}
func (r *recordingAPI) DeleteTunnel(_ context.Context, _, tunnelID string) error {
	r.deleted = append(r.deleted, tunnelID)
	return r.deleteErr
}
func (r *recordingAPI) GetTunnelConfig(context.Context, string, string) ([]Route, error) {
	return nil, nil
}

func (r *recordingAPI) PutTunnelConfig(_ context.Context, _, _ string, routes []Route) error {
	r.putRoutes = routes
	return r.putErr
}
func (r *recordingAPI) ListDNSRecords(context.Context, string) ([]DNSRecord, error) {
	return r.dnsRecords, nil
}
func (r *recordingAPI) Connections(context.Context, string, string) ([]Connector, error) {
	return nil, nil
}

var _ API = (*recordingAPI)(nil)

func (r *recordingAPI) GetTunnelToken(_ context.Context, _, tunnelID string) (string, error) {
	if r.tokenErr != nil {
		return "", r.tokenErr
	}
	return r.token, nil
}

// TestTunnelTokenUsesAPILookup checks the remote backend fetches the connector
// token from the API rather than shelling out to cloudflared.
func TestTunnelTokenUsesAPILookup(t *testing.T) {
	rec := &recordingAPI{token: "token-abc"}
	backend, err := NewRemoteBackend("acct-1", "zone-1", rec)
	if err != nil {
		t.Fatalf("NewRemoteBackend: %v", err)
	}
	tok, err := backend.TunnelToken(context.Background(), "tunnel-1")
	if err != nil {
		t.Fatalf("TunnelToken: %v", err)
	}
	if tok != "token-abc" {
		t.Fatalf("token = %q, want token-abc", tok)
	}
}

// TestTunnelTokenPropagatesAPIError keeps a failed lookup from looking like an
// empty token.
func TestTunnelTokenPropagatesAPIError(t *testing.T) {
	sentinel := errors.New("boom")
	rec := &recordingAPI{tokenErr: sentinel}
	backend, err := NewRemoteBackend("acct-1", "zone-1", rec)
	if err != nil {
		t.Fatalf("NewRemoteBackend: %v", err)
	}
	if _, err := backend.TunnelToken(context.Background(), "tunnel-1"); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}

func TestPutRoutesKeepsCatchAll(t *testing.T) {
	cases := []struct {
		name   string
		in     []Route
		last   string
		totalN int
	}{
		{
			name: "catch-all absent is appended last",
			in: []Route{
				{Hostname: "app.example.com", Service: "http://localhost:3000"},
				{Hostname: "api.example.com", Service: "http://localhost:8080"},
			},
			last:   DefaultCatchAll,
			totalN: 3,
		},
		{
			name: "catch-all in the middle is moved last",
			in: []Route{
				{Service: DefaultCatchAll},
				{Hostname: "app.example.com", Service: "http://localhost:3000"},
			},
			last:   DefaultCatchAll,
			totalN: 2,
		},
		{
			name: "duplicate catch-alls collapse to one",
			in: []Route{
				{Service: DefaultCatchAll},
				{Hostname: "app.example.com", Service: "http://localhost:3000"},
				{Service: DefaultCatchAll},
			},
			last:   DefaultCatchAll,
			totalN: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingAPI{}
			backend, err := NewRemoteBackend("acct-1", "zone-1", rec)
			if err != nil {
				t.Fatalf("NewRemoteBackend: %v", err)
			}
			if err := backend.PutRoutes(context.Background(), "tunnel-1", tc.in); err != nil {
				t.Fatalf("PutRoutes: %v", err)
			}
			if len(rec.putRoutes) != tc.totalN {
				t.Fatalf("sent %d routes, want %d: %+v", len(rec.putRoutes), tc.totalN, rec.putRoutes)
			}
			last := rec.putRoutes[len(rec.putRoutes)-1]
			if last.Service != tc.last {
				t.Errorf("last route service = %q, want catch-all %q", last.Service, tc.last)
			}
			n := 0
			for _, r := range rec.putRoutes {
				if IsCatchAll(r) {
					n++
				}
			}
			if n != 1 {
				t.Errorf("route list contains %d catch-alls, want exactly 1", n)
			}
		})
	}
}

func TestPutRoutesPropagatesAPIError(t *testing.T) {
	sentinel := errors.New("boom")
	rec := &recordingAPI{putErr: sentinel}
	backend, err := NewRemoteBackend("acct-1", "zone-1", rec)
	if err != nil {
		t.Fatalf("NewRemoteBackend: %v", err)
	}
	if err := backend.PutRoutes(context.Background(), "tunnel-1", nil); !errors.Is(err, sentinel) {
		t.Fatalf("PutRoutes error = %v, want %v", err, sentinel)
	}
}

func TestSyncDNSPlansChangesFromLiveRecords(t *testing.T) {
	const tunnelID = "t1"
	rec := &recordingAPI{dnsRecords: []DNSRecord{
		{Name: "live.example.com", Type: "CNAME", Content: TunnelCNAME(tunnelID), OwnerTunnel: tunnelID},
		{Name: "stale.example.com", Type: "CNAME", Content: TunnelCNAME(tunnelID), OwnerTunnel: tunnelID},
		{Name: "new.example.com", Type: "CNAME", Content: "old.cfargotunnel.com", OwnerTunnel: tunnelID},
	}}
	backend, err := NewRemoteBackend("acct-1", "zone-1", rec)
	if err != nil {
		t.Fatalf("NewRemoteBackend: %v", err)
	}

	routes := []Route{
		{Hostname: "live.example.com", Service: "http://localhost:3000"},
		{Hostname: "new.example.com", Service: "http://localhost:4000"},
		{Hostname: "absent.example.com", Service: "http://localhost:5000"},
		{Service: "http_status:404"},
	}
	changes, err := backend.SyncDNS(context.Background(), tunnelID, routes)
	if err != nil {
		t.Fatalf("SyncDNS: %v", err)
	}

	got := map[string]string{}
	for _, c := range changes {
		got[c.Hostname] = c.Action
	}
	want := map[string]string{
		"new.example.com":    "update",
		"absent.example.com": "create",
		"stale.example.com":  "orphan",
	}
	if len(got) != len(want) {
		t.Fatalf("SyncDNS returned %d changes %+v, want %d", len(got), got, len(want))
	}
	for host, action := range want {
		if got[host] != action {
			t.Errorf("%s: action = %q, want %q", host, got[host], action)
		}
	}
	if _, reported := got["live.example.com"]; reported {
		t.Errorf("a correct record must not be reported as a change: %+v", got)
	}
}

// TestRemoteCreateTunnelCallsAPI proves the remote backend can create a tunnel
// through the API without any local cloudflared invocation.
func TestRemoteCreateTunnelCallsAPI(t *testing.T) {
	rec := &recordingAPI{}
	b, err := NewRemoteBackend("acct-1", "zone-1", rec)
	if err != nil {
		t.Fatalf("NewRemoteBackend: %v", err)
	}
	got, err := b.CreateTunnel(context.Background(), "fana")
	if err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}
	if len(rec.created) != 1 || rec.created[0] != "fana" {
		t.Fatalf("api created = %v, want [fana]", rec.created)
	}
	if got.Name != "fana" {
		t.Fatalf("returned tunnel = %+v, want Name fana", got)
	}
}

// TestRemoteDeleteTunnelCallsAPI covers the delete path.
func TestRemoteDeleteTunnelCallsAPI(t *testing.T) {
	rec := &recordingAPI{}
	b, err := NewRemoteBackend("acct-1", "zone-1", rec)
	if err != nil {
		t.Fatalf("NewRemoteBackend: %v", err)
	}
	if err := b.DeleteTunnel(context.Background(), "tid-9"); err != nil {
		t.Fatalf("DeleteTunnel: %v", err)
	}
	if len(rec.deleted) != 1 || rec.deleted[0] != "tid-9" {
		t.Fatalf("api deleted = %v, want [tid-9]", rec.deleted)
	}
}

// TestRemoteCreateTunnelSurfacesAPIError keeps API failures visible.
func TestRemoteCreateTunnelSurfacesAPIError(t *testing.T) {
	rec := &recordingAPI{createErr: errors.New("name already exists")}
	b, err := NewRemoteBackend("acct-1", "zone-1", rec)
	if err != nil {
		t.Fatalf("NewRemoteBackend: %v", err)
	}
	if _, err := b.CreateTunnel(context.Background(), "dup"); err == nil {
		t.Fatal("a failing API create must surface an error")
	}
}
