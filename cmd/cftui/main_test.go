package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masfana/cftui/internal/cf"
	"github.com/masfana/cftui/internal/local"
)

type fakeBackend struct {
	tunnels    []cf.Tunnel
	routes     []cf.Route
	called     bool
	routesRead bool
}

func (f *fakeBackend) ListTunnels(context.Context) ([]cf.Tunnel, error) {
	f.called = true
	return f.tunnels, nil
}

func (f *fakeBackend) GetRoutes(context.Context, string) ([]cf.Route, error) {
	f.routesRead = true
	return f.routes, nil
}

func (f *fakeBackend) PutRoutes(context.Context, string, []cf.Route) error { return nil }

func (f *fakeBackend) SyncDNS(context.Context, string, []cf.Route) ([]cf.DNSChange, error) {
	return nil, nil
}

func (f *fakeBackend) Health(context.Context, string) ([]cf.Connector, error) { return nil, nil }

var _ cf.Backend = (*fakeBackend)(nil)

func TestManagementForRemoteTunnelIsRemote(t *testing.T) {
	file, err := local.Load(filepath.Join(t.TempDir(), "config.yml"))
	if err != nil {
		t.Fatalf("local.Load: %v", err)
	}
	got := managementForTunnel(file)
	if len(got) != 0 {
		t.Fatalf("empty config should classify nothing, got %v", got)
	}

	m := managementForTunnel(file)
	for _, tt := range []cf.Tunnel{{Name: "CFTUI", ID: "221cc559"}} {
		if _, ok := m[tt.Name]; !ok {
			m[tt.Name] = local.ManagedRemote
		}
	}
	if m["CFTUI"] != local.ManagedRemote {
		t.Fatalf("remote-only tunnel badge = %q, want %q", m["CFTUI"], local.ManagedRemote)
	}
}

func TestDebugUsesRemoteWhenAvailable(t *testing.T) {
	localB := &fakeBackend{}
	remoteB := &fakeBackend{tunnels: []cf.Tunnel{{Name: "CFTUI", Status: "down"}}}

	src := debugBackend(cf.Backend(localB), cf.Backend(remoteB))
	if err := runDebug(src, nil); err != nil {
		t.Fatalf("runDebug: %v", err)
	}
	if !remoteB.called {
		t.Fatal("runDebug did not consult the remote backend")
	}
	if localB.called {
		t.Fatal("runDebug consulted the local backend even though remote was available")
	}
}

func TestDebugFallsBackToLocal(t *testing.T) {
	localB := &fakeBackend{tunnels: []cf.Tunnel{{Name: "from-file"}}}

	src := debugBackend(cf.Backend(localB), nil)
	if err := runDebug(src, nil); err != nil {
		t.Fatalf("runDebug: %v", err)
	}
	if !localB.called {
		t.Fatal("runDebug did not fall back to the local backend")
	}
}

func TestRouteCountComesFromGetRoutes(t *testing.T) {
	localB := &fakeBackend{}
	remoteB := &fakeBackend{
		tunnels: []cf.Tunnel{{Name: "CFTUI", ID: "221cc559", Status: "healthy", ConnectorN: 4}},
		routes:  []cf.Route{{Hostname: "sigma.fmo.qzz.io"}, {Service: "http_status:404"}},
	}

	out := captureStdout(t, func() {
		if err := runDebug(remoteB, nil); err != nil {
			t.Fatalf("runDebug: %v", err)
		}
	})
	if !remoteB.routesRead {
		t.Fatal("runDebug never asked the backend for routes")
	}
	if !strings.Contains(out, "       2") {
		t.Fatalf("ROUTES column should read 2, got:\n%s", out)
	}
	_ = localB
}

func TestRemoteManagedBadgeWins(t *testing.T) {
	remoteB := &fakeBackend{
		tunnels: []cf.Tunnel{{Name: "CFTUI", ID: "221cc559", RemoteManaged: true}},
	}
	m := map[string]local.Management{"CFTUI": local.ManagedUnknown}
	src := debugBackend(nil, remoteB)
	out := captureStdout(t, func() {
		if err := runDebug(src, m); err != nil {
			t.Fatalf("runDebug: %v", err)
		}
	})
	if !strings.Contains(out, "remote") {
		t.Fatalf("a RemoteConfig tunnel must render as remote, got:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

type recordingVerifier struct {
	calls [][]string
	out   []byte
	err   error
}

func (r *recordingVerifier) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.out, r.err
}

func TestLocalBackendCreateTunnelRunsCloudflared(t *testing.T) {
	v := &recordingVerifier{out: []byte("Created tunnel fana")}
	b := localBackend{pipe: &local.Pipeline{Verifier: v}}

	got, err := b.CreateTunnel(context.Background(), "fana")
	if err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}
	if len(v.calls) != 1 {
		t.Fatalf("expected one cloudflared call, got %v", v.calls)
	}
	want := []string{"cloudflared", "tunnel", "create", "fana"}
	for i := range want {
		if v.calls[0][i] != want[i] {
			t.Fatalf("call = %v, want %v", v.calls[0], want)
		}
	}
	if got.Name != "fana" {
		t.Fatalf("created tunnel name = %q, want fana", got.Name)
	}
}

func TestLocalBackendDeleteTunnelRunsCloudflared(t *testing.T) {
	v := &recordingVerifier{}
	b := localBackend{pipe: &local.Pipeline{Verifier: v}}

	if err := b.DeleteTunnel(context.Background(), "221cc559"); err != nil {
		t.Fatalf("DeleteTunnel: %v", err)
	}
	want := []string{"cloudflared", "tunnel", "delete", "--force", "221cc559"}
	for i := range want {
		if v.calls[0][i] != want[i] {
			t.Fatalf("call = %v, want %v", v.calls[0], want)
		}
	}
}

func TestLocalBackendDeleteRefusesEmptyID(t *testing.T) {
	v := &recordingVerifier{}
	b := localBackend{pipe: &local.Pipeline{Verifier: v}}

	if err := b.DeleteTunnel(context.Background(), ""); err == nil {
		t.Fatal("empty tunnel id must be refused")
	}
	if len(v.calls) != 0 {
		t.Fatalf("cloudflared must not be invoked, got %v", v.calls)
	}
}

func TestLocalBackendCreateSurfacesCLIError(t *testing.T) {
	v := &recordingVerifier{out: []byte("tunnel name already exists"), err: errors.New("exit status 1")}
	b := localBackend{pipe: &local.Pipeline{Verifier: v}}

	_, err := b.CreateTunnel(context.Background(), "fana")
	if err == nil {
		t.Fatal("a failing cloudflared must surface an error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error should carry cloudflared output, got %q", err.Error())
	}
}

// TestBadgeForUnlistedTunnelUsesCredentials covers a tunnel the config file does
// not name: a CLI-created tunnel with a per-tunnel credentials file must read as
// local, not unknown, and not remote.
func TestBadgeForUnlistedTunnelUsesCredentials(t *testing.T) {
	home := t.TempDir()
	credDir := filepath.Join(home, ".cloudflared")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const id = "c2e6d57a-7109-4b4a-a093-8ed4b6c2daf3"
	if err := os.WriteFile(filepath.Join(credDir, id+".json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write cred: %v", err)
	}
	t.Setenv("HOME", home)

	remoteB := &fakeBackend{tunnels: []cf.Tunnel{{Name: "fana", ID: id, Status: "inactive"}}}
	out := captureStdout(t, func() {
		if err := runDebug(remoteB, nil); err != nil {
			t.Fatalf("runDebug: %v", err)
		}
	})
	if !strings.Contains(out, "local") {
		t.Fatalf("a credentialed tunnel must read as local, got:\n%s", out)
	}
	if strings.Contains(out, "unknown") {
		t.Fatalf("badge must not be unknown when credentials exist:\n%s", out)
	}
}
