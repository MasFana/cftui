package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MasFana/cftui/internal/cf"
)

// fakeBackend is a scripted cf.Backend with no network access.
type fakeBackend struct {
	tunnels    []cf.Tunnel
	routes     map[string][]cf.Route
	err        error
	created    []string
	deleted    []string
	puts       int
	token      string
	tokenIDs   []string
	tokenErr   error
	hasTokenFn bool
}

func (f *fakeBackend) ListTunnels(context.Context) ([]cf.Tunnel, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tunnels, nil
}

func (f *fakeBackend) GetRoutes(_ context.Context, tunnelID string) ([]cf.Route, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.routes[tunnelID], nil
}

func (f *fakeBackend) PutRoutes(_ context.Context, tunnelID string, routes []cf.Route) error {
	f.puts++
	if f.err == nil {
		f.routes[tunnelID] = append([]cf.Route(nil), routes...)
	}
	return f.err
}

func (f *fakeBackend) SyncDNS(context.Context, string, []cf.Route) ([]cf.DNSChange, error) {
	return nil, f.err
}

func (f *fakeBackend) Health(context.Context, string) ([]cf.Connector, error) {
	return nil, f.err
}

func (f *fakeBackend) CreateTunnel(_ context.Context, name string) (cf.Tunnel, error) {
	if f.err != nil {
		return cf.Tunnel{}, f.err
	}
	t := cf.Tunnel{ID: name, Name: name}
	f.created = append(f.created, name)
	f.tunnels = append(f.tunnels, t)
	return t, nil
}

func (f *fakeBackend) DeleteTunnel(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeBackend) TunnelToken(_ context.Context, id string) (string, error) {
	f.tokenIDs = append(f.tokenIDs, id)
	if f.tokenErr != nil {
		return "", f.tokenErr
	}
	return f.token, nil
}

// plainBackend serves the read-only cf.Backend surface but deliberately has no
// TunnelToken, standing in for a backend that can only fetch tokens via CLI. It
// delegates explicitly rather than embedding fakeBackend, so the token method is
// not promoted onto it.
type plainBackend struct{ base *fakeBackend }

func (p plainBackend) ListTunnels(ctx context.Context) ([]cf.Tunnel, error) {
	return p.base.ListTunnels(ctx)
}
func (p plainBackend) GetRoutes(ctx context.Context, tunnelID string) ([]cf.Route, error) {
	return p.base.GetRoutes(ctx, tunnelID)
}
func (p plainBackend) PutRoutes(ctx context.Context, tunnelID string, routes []cf.Route) error {
	return p.base.PutRoutes(ctx, tunnelID, routes)
}
func (p plainBackend) SyncDNS(ctx context.Context, tunnelID string, routes []cf.Route) ([]cf.DNSChange, error) {
	return p.base.SyncDNS(ctx, tunnelID, routes)
}
func (p plainBackend) Health(ctx context.Context, tunnelID string) ([]cf.Connector, error) {
	return p.base.Health(ctx, tunnelID)
}

var _ cf.Backend = (*fakeBackend)(nil)

func newTestModel() Model {
	f := &fakeBackend{
		tunnels: []cf.Tunnel{
			{ID: "t1", Name: "app-tunnel", Status: "healthy", ConnectorN: 2, RouteN: 2},
			{ID: "t2", Name: "web-tunnel", Status: "down"},
		},
		routes: map[string][]cf.Route{
			"t1": {
				{Hostname: "app.example.com", Service: "http://localhost:3000"},
				{Service: "http_status:404"},
			},
		},
	}
	return New(Backends{Remote: f, Local: f})
}

// newCLIModel builds a model whose backends cannot fetch a connector token
// through the API, so the t action must use the cloudflared CLI path that
// withTokenFetch stubs.
func newCLIModel() Model {
	base := &fakeBackend{
		tunnels: []cf.Tunnel{
			{ID: "t1", Name: "app-tunnel", Status: "healthy", ConnectorN: 2, RouteN: 2},
			{ID: "t2", Name: "web-tunnel", Status: "down"},
		},
	}
	b := plainBackend{base: base}
	return New(Backends{Remote: b, Local: b})
}

func apply(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want ui.Model", next)
	}
	return got
}

func sendList(t *testing.T, m Model) Model {
	t.Helper()
	msg := m.Init()()
	return apply(t, m, msg)
}

func key(s string) tea.KeyMsg {
	if len([]rune(s)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestInitRendersTunnelList(t *testing.T) {
	m := sendList(t, newTestModel())
	out := m.View()
	for _, want := range []string{"cftui - tunnels", "app-tunnel", "web-tunnel"} {
		if !strings.Contains(out, want) {
			t.Errorf("tunnel list missing %q:\n%s", want, out)
		}
	}
}

func TestEnterOpensRouteTableWithDetail(t *testing.T) {
	m := sendList(t, newTestModel())
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter must return a command that loads routes")
	}
	m = apply(t, m, cmd())

	out := m.View()
	for _, want := range []string{"routes - app-tunnel", "app.example.com", "(catch-all)", "http_status:404", "detail"} {
		if !strings.Contains(out, want) {
			t.Errorf("route view missing %q:\n%s", want, out)
		}
	}
}

func TestAddRouteFormOpensOnPortField(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("a"))
	if m.view != ViewForm {
		t.Fatalf("view = %v, want ViewForm", m.view)
	}
	if got := m.fieldName(); got != "port" {
		t.Fatalf("focused field = %q, want port (self-port fast path)", got)
	}
	if out := m.View(); !strings.Contains(out, "local port first") {
		t.Errorf("form view missing the port-first heading:\n%s", out)
	}
}

func TestTypingPortTriggersProbe(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("a"))

	next, cmd := m.Update(key("3"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("typing a port must schedule a probe")
	}
	msg, ok := cmd().(probeMsg)
	if !ok {
		t.Fatalf("probe command returned %T, want probeMsg", msg)
	}
	if msg.port != "3" {
		t.Fatalf("probe port = %q, want 3", msg.port)
	}
	m = apply(t, m, msg)
	if !m.probed {
		t.Fatal("probed flag not set after probeMsg")
	}
	if out := m.View(); !strings.Contains(out, "localhost:3") {
		t.Errorf("form does not report the probe in the view:\n%s", out)
	}
}

func TestStaleProbeIsIgnored(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("a"))
	m = apply(t, m, key("8"))
	stale := probeMsg{port: "80", res: m.probe}
	m = apply(t, m, stale)
	if m.probed {
		t.Fatal("a probe for a different port must be ignored")
	}
}

func TestCommitStagesRouteBeforeCatchAll(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("enter"))
	m = apply(t, m, routeListMsg{tunnel: m.tunnels[0], routes: []cf.Route{
		{Hostname: "skip.org", Service: "http://localhost:1234"},
		{Hostname: "taken.example.com", Service: "http://localhost:3000"},
		{Service: "http_status:404"},
	}})
	if len(m.routes) != 3 {
		t.Fatalf("route table not loaded: %+v", m.routes)
	}
	m = apply(t, m, key("a"))

	for _, r := range []string{"3", "0", "0", "0"} {
		m = apply(t, m, key(r))
	}
	m = apply(t, m, key("tab"))
	for _, r := range "new.example.com" {
		m = apply(t, m, key(string(r)))
	}
	m = apply(t, m, key("enter"))

	if m.view != ViewRoutes {
		t.Fatalf("view = %v, want ViewRoutes after commit", m.view)
	}
	if len(m.routes) != 4 {
		t.Fatalf("want 4 routes, got %d: %+v", len(m.routes), m.routes)
	}
	if got := m.routes[2].Service; got != "http://localhost:3000" {
		t.Errorf("staged route service = %q, want http://localhost:3000", got)
	}
	if last := m.routes[3]; last.Hostname != "" || last.Service != "http_status:404" {
		t.Errorf("catch-all must stay last, got %+v", last)
	}
	if out := m.View(); !strings.Contains(out, "staged") && !strings.Contains(out, "new.example.com") {
		t.Errorf("commit did not surface a status or the new route:\n%s", out)
	}
}

func TestCommitRejectsBadPortWithoutStaging(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("a"))
	for _, r := range []string{"9", "9", "9", "9", "9"} {
		m = apply(t, m, key(r))
	}
	before := len(m.routes)
	m = apply(t, m, key("enter"))
	if m.view != ViewForm {
		t.Fatalf("view = %v, want to stay on ViewForm after a bad port", m.view)
	}
	if len(m.routes) != before {
		t.Fatal("a bad port must not stage a route")
	}
	if m.errText == "" {
		t.Fatal("a bad port must surface an error")
	}
}

func TestBackendErrorSurfacesInView(t *testing.T) {
	f := &fakeBackend{err: errors.New("boom")}
	m := New(Backends{Local: f})
	m = sendList(t, m)
	if out := m.View(); !strings.Contains(out, "boom") {
		t.Errorf("backend error not shown:\n%s", out)
	}
}

func TestQuitOnQ(t *testing.T) {
	m := sendList(t, newTestModel())
	next, cmd := m.Update(key("q"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("q must return tea.Quit")
	}
	if m.View() != "" {
		t.Errorf("a quitting model must render nothing, got %q", m.View())
	}
}

func TestCreateTunnelFormValidatesName(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("n"))
	m = apply(t, m, key("Bad_Name"))
	m = apply(t, m, key("enter"))
	if m.errText == "" || m.mode != "create-tunnel" {
		t.Fatal("invalid name must stay in create form")
	}
	m.confirmName = "new-tunnel"
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("valid name must create tunnel")
	}
	m = apply(t, m, cmd())
	if m.status != "tunnel created" {
		t.Fatalf("status = %q", m.status)
	}
}

func TestDeleteTunnelRequiresTypedName(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("d"))
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil || m.errText == "" {
		t.Fatal("empty confirmation must refuse delete")
	}
	for _, r := range "app-tunnel" {
		m = apply(t, m, key(string(r)))
	}
	next, cmd = m.Update(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("exact name must release delete")
	}
	m = apply(t, m, cmd())
	if m.status != "tunnel deleted" {
		t.Fatalf("status = %q", m.status)
	}
}

func TestDuplicateRouteRejected(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("enter"))
	m = apply(t, m, routeListMsg{tunnel: m.tunnels[0], routes: m.backends.Local.(*fakeBackend).routes["t1"]})
	m = apply(t, m, key("a"))
	m.form.Port = "3000"
	m.form.Hostname = "APP.EXAMPLE.COM"
	m = apply(t, m, key("enter"))
	if m.errText == "" || m.view != ViewForm {
		t.Fatal("duplicate must remain form with error")
	}
}

func TestReorderKeepsCatchAllLast(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("enter"))
	m = apply(t, m, routeListMsg{tunnel: m.tunnels[0], routes: m.backends.Local.(*fakeBackend).routes["t1"]})
	m.routeAt = 0
	m = apply(t, m, key("J"))
	if !cf.IsCatchAll(m.routes[len(m.routes)-1]) {
		t.Fatal("catch-all must remain last")
	}
}

func TestCatchAllNotReorderable(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("enter"))
	m = apply(t, m, routeListMsg{tunnel: m.tunnels[0], routes: m.backends.Local.(*fakeBackend).routes["t1"]})
	m.routeAt = len(m.routes) - 1
	before := append([]cf.Route(nil), m.routes...)
	m = apply(t, m, key("K"))
	if Changed(before, m.routes) {
		t.Fatal("catch-all must not move")
	}
}

func TestApplyStagesThroughPutRoutes(t *testing.T) {
	m := sendList(t, newTestModel())
	f := m.backends.Local.(*fakeBackend)
	m = apply(t, m, key("enter"))
	m = apply(t, m, routeListMsg{tunnel: m.tunnels[0], routes: f.routes["t1"]})
	m.routes[0].Service = "http://localhost:4000"
	next, cmd := m.Update(key("w"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("write must call backend")
	}
	m = apply(t, m, cmd())
	if f.puts != 1 {
		t.Fatalf("puts=%d", f.puts)
	}
}

func TestDiscardRevertsStaging(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("enter"))
	m = apply(t, m, routeListMsg{tunnel: m.tunnels[0], routes: m.backends.Local.(*fakeBackend).routes["t1"]})
	before := m.committed[0]
	m.routes[0].Service = "http://localhost:4000"
	m = apply(t, m, key("u"))
	m = apply(t, m, key("y"))
	if m.routes[0] != before {
		t.Fatal("discard must restore committed routes")
	}
}

func TestQuitWarnsOnUnsavedChanges(t *testing.T) {
	m := sendList(t, newTestModel())
	m = apply(t, m, key("enter"))
	m = apply(t, m, routeListMsg{tunnel: m.tunnels[0], routes: m.backends.Local.(*fakeBackend).routes["t1"]})
	m.routes[0].Service = "http://localhost:4000"
	next, cmd := m.Update(key("q"))
	m = next.(Model)
	if cmd != nil || m.errText == "" {
		t.Fatal("first q must warn")
	}
	_, cmd = m.Update(key("q"))
	if cmd == nil {
		t.Fatal("second q must quit")
	}
}

// adminTrackingBackend counts create/delete calls so a test can prove which
// backend the model actually used. The plain fakeBackend in this file cannot,
// because newTestModel installs the same value for Remote and Local.
type adminTrackingBackend struct {
	*fakeBackend
	created []string
	deleted []string
}

func newAdminTracking() *adminTrackingBackend {
	return &adminTrackingBackend{fakeBackend: &fakeBackend{
		tunnels: []cf.Tunnel{{ID: "t1", Name: "app-tunnel", Status: "healthy"}},
	}}
}

func (a *adminTrackingBackend) CreateTunnel(_ context.Context, name string) (cf.Tunnel, error) {
	a.created = append(a.created, name)
	return cf.Tunnel{ID: name, Name: name}, nil
}

func (a *adminTrackingBackend) DeleteTunnel(_ context.Context, id string) error {
	a.deleted = append(a.deleted, id)
	return nil
}

// TestCreatePrefersRemoteBackend locks in the fix for "backend does not support
// tunnel creation": when a remote backend exists it must be the one asked to
// create, never the local file backend.
func TestCreatePrefersRemoteBackend(t *testing.T) {
	remote := newAdminTracking()
	localB := newAdminTracking()
	m := New(Backends{Remote: remote, Local: localB})
	m = sendList(t, m)

	m = apply(t, m, key("n"))
	m.confirmName = "fana"
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("create must dispatch a command, errText=%q", m.errText)
	}
	m = apply(t, m, cmd())

	if len(remote.created) != 1 || remote.created[0] != "fana" {
		t.Fatalf("remote backend created %v, want [fana]", remote.created)
	}
	if len(localB.created) != 0 {
		t.Fatalf("local backend was used for creation: %v", localB.created)
	}
	if strings.Contains(m.errText, "does not support") {
		t.Fatalf("unexpected unsupported error: %q", m.errText)
	}
}

// TestDeletePrefersRemoteBackend is the delete-side counterpart.
func TestDeletePrefersRemoteBackend(t *testing.T) {
	remote := newAdminTracking()
	localB := newAdminTracking()
	m := New(Backends{Remote: remote, Local: localB})
	m = sendList(t, m)

	m = apply(t, m, key("d"))
	for _, r := range "app-tunnel" {
		m = apply(t, m, key(string(r)))
	}
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("delete must dispatch a command, errText=%q", m.errText)
	}
	m = apply(t, m, cmd())

	if len(remote.deleted) != 1 {
		t.Fatalf("remote backend deleted %v, want one call", remote.deleted)
	}
	if len(localB.deleted) != 0 {
		t.Fatalf("local backend was used for deletion: %v", localB.deleted)
	}
}

// TestCreateFallsBackToLocalWhenNoRemote covers the cert-free path: with no
// remote backend the local CLI-backed implementation must be used.
func TestCreateFallsBackToLocalWhenNoRemote(t *testing.T) {
	localB := newAdminTracking()
	m := New(Backends{Local: localB})
	m = sendList(t, m)

	m = apply(t, m, key("n"))
	m.confirmName = "fana"
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("create must dispatch a command, errText=%q", m.errText)
	}
	m = apply(t, m, cmd())

	if len(localB.created) != 1 || localB.created[0] != "fana" {
		t.Fatalf("local backend created %v, want [fana]", localB.created)
	}
}

// TestListReadsFromRemoteNotLocal pins the bug where Init() read tunnels from
// the local backend only. The local backend has no config.yml in a real
// cert-based setup, so the list came up empty while the remote backend held
// every tunnel.
func TestListReadsFromRemoteNotLocal(t *testing.T) {
	remote := &fakeBackend{tunnels: []cf.Tunnel{{ID: "r1", Name: "from-remote"}}}
	local := &fakeBackend{tunnels: []cf.Tunnel{{ID: "l1", Name: "from-local"}}}
	m := New(Backends{Remote: remote, Local: local})
	m = sendList(t, m)

	if len(m.tunnels) != 1 || m.tunnels[0].Name != "from-remote" {
		t.Fatalf("list = %+v, want the remote backend's tunnel", m.tunnels)
	}
}

// TestListFallsBackToLocalWhenNoRemote keeps the offline path working.
func TestListFallsBackToLocalWhenNoRemote(t *testing.T) {
	local := &fakeBackend{tunnels: []cf.Tunnel{{ID: "l1", Name: "from-local"}}}
	m := New(Backends{Local: local})
	m = sendList(t, m)

	if len(m.tunnels) != 1 || m.tunnels[0].Name != "from-local" {
		t.Fatalf("list = %+v, want the local backend's tunnel", m.tunnels)
	}
}

// TestReadBackendPrefersRemote is the direct unit test for the selector.
func TestReadBackendPrefersRemote(t *testing.T) {
	remote := &fakeBackend{}
	local := &fakeBackend{}
	if got := (Backends{Remote: remote, Local: local}).readBackend(); got != cf.Backend(remote) {
		t.Fatalf("readBackend = %v, want remote", got)
	}
	if got := (Backends{Local: local}).readBackend(); got != cf.Backend(local) {
		t.Fatalf("readBackend with no remote = %v, want local", got)
	}
}

// TestCreateViaRemoteAdminRealInterface drives the create path with a backend
// that implements the tunnelAdmin interface the way the real RemoteBackend does.
// Before the fix the UI asserted only the local backend, so this printed
// "backend does not support tunnel creation" even though the remote backend
// could create.
func TestCreateViaRemoteAdminRealInterface(t *testing.T) {
	remote := &adminTrackingBackend{fakeBackend: &fakeBackend{}}
	local := &fakeBackend{}
	m := New(Backends{Remote: remote, Local: local})

	m = apply(t, m, key("n"))
	m.confirmName = "fana"
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("create must dispatch a command, errText=%q", m.errText)
	}
	m = apply(t, m, cmd())

	if m.errText == "backend does not support tunnel creation" {
		t.Fatal("create still reports unsupported with a remote admin present")
	}
	if len(remote.created) != 1 || remote.created[0] != "fana" {
		t.Fatalf("remote created = %v, want [fana]", remote.created)
	}
}

// TestDeleteDoesNotPanicOnEmptyList guards the delete handler against an empty
// tunnel list. Pressing 'd' with no tunnels, or deleting the last tunnel and
// then confirming again, must not index out of range.
func TestDeleteDoesNotPanicOnEmptyList(t *testing.T) {
	m := New(Backends{Local: &fakeBackend{}})
	m.mode = "delete-tunnel"
	m.confirmName = "ghost"
	m.tunnels = nil

	next, _ := m.Update(key("enter"))
	if got := next.(Model); got.errText == "" {
		t.Fatalf("expected a guard error, got errText=%q", got.errText)
	}
}

// TestDeletePromptOnEmptyListDoesNotPanic covers the render path: pressing 'd'
// on an empty list must not index the tunnel slice while drawing the prompt.
func TestDeletePromptOnEmptyListDoesNotPanic(t *testing.T) {
	m := New(Backends{Local: &fakeBackend{}})
	m.mode = "delete-tunnel"
	m.tunnels = nil

	_ = m.View()
}

// TestTunnelListHasHelpKey checks the list responds to ? like every other view.
func TestTunnelListHasHelpKey(t *testing.T) {
	m := newTestModel()
	m = sendList(t, m)
	m = apply(t, m, key("?"))

	if !m.showHelp {
		t.Fatal("? must toggle help on the tunnel list")
	}
	if !strings.Contains(m.View(), "help") {
		t.Fatal("help must render on the tunnel list")
	}
}

// TestEscFromRoutesReturnsToTunnels is the reported bug.
func TestEscFromRoutesReturnsToTunnels(t *testing.T) {
	m := newTestModel()
	m = sendList(t, m)
	m = apply(t, m, key("enter"))
	if m.view != ViewRoutes {
		t.Fatalf("enter should open routes, view=%v", m.view)
	}
	m = apply(t, m, key("esc"))
	if m.view != ViewTunnels {
		t.Fatalf("esc from routes must return to the tunnel list, view=%v", m.view)
	}
}

// TestHelpDoesNotDuplicateFooter keeps the full help panel free of the short
// key footer underneath it.
func TestHelpDoesNotDuplicateFooter(t *testing.T) {
	m := newTestModel()
	m = sendList(t, m)

	send := func(k string) {
		m = apply(t, m, key(k))
	}
	send("?")
	if !strings.Contains(m.View(), "cftui key reference") {
		t.Fatal("? must show the full key reference")
	}
	if strings.Contains(m.View(), "n new tunnel  d delete tunnel  enter open  ? help") {
		t.Fatal("full help must not also render the short footer")
	}

	send("?")
	if !strings.Contains(m.View(), "n new tunnel") {
		t.Fatal("closing help must restore the short footer")
	}
}

// TestAddRouteRequiresTunnelContext stops the form from staging a route with
// no tunnel behind it. Pressing 'a' from the tunnel list used to open the form,
// stage the route into a phantom list, and jump to a view titled "routes - ".
func TestAddRouteRequiresTunnelContext(t *testing.T) {
	f := &fakeBackend{tunnels: []cf.Tunnel{{ID: "t1", Name: "app"}}}
	m := New(Backends{Local: f})
	m = sendList(t, m)

	// 'a' from the list must select the cursor's tunnel, not stay empty.
	m = apply(t, m, key("a"))
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil {
		m = apply(t, m, cmd())
	}
	if m.tunnel.ID == "" {
		t.Fatalf("staging without a tunnel must be refused; view=%v routes=%d", m.view, len(m.routes))
	}
}

// TestCommitRefusesWithoutTunnel is the direct guard on commit().
func TestCommitRefusesWithoutTunnel(t *testing.T) {
	m := New(Backends{Local: &fakeBackend{}})
	m.form.Port = "3000"
	m.form.Hostname = "x.example.com"

	next, _ := m.commit()
	got := next.(Model)
	if got.errText == "" {
		t.Fatal("commit without a tunnel must error")
	}
	if got.view != ViewTunnels {
		t.Fatalf("commit without a tunnel should return to the list, view=%v", got.view)
	}
}

// TestEscFromFormReturnsToRoutes keeps navigation symmetric: the form is
// reached from the routes view, so canceling returns there.
func TestEscFromFormReturnsToRoutes(t *testing.T) {
	m := newTestModel()
	m = sendList(t, m)
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil {
		m = apply(t, m, cmd())
	}
	m = apply(t, m, key("a"))
	if m.view != ViewForm {
		t.Fatalf("a should open the form, view=%v", m.view)
	}
	m = apply(t, m, key("esc"))
	if m.view != ViewRoutes {
		t.Fatalf("esc from form should return to routes, view=%v", m.view)
	}
}

// TestHelpIsModal is the regression guard for the reported '?' bug. The help
// panel covered the screen but every key kept firing underneath it: 'd' opened
// the delete prompt, 'n' started tunnel creation, and 'w' applied staged route
// changes - all invisible behind the overlay. A help panel must swallow input.
func TestHelpIsModal(t *testing.T) {
	m := newTestModel()
	m = sendList(t, m)
	m = apply(t, m, key("?"))

	// Every action key must be inert while help is open.
	for _, k := range []string{"n", "d", "w", "enter", "r", "a", "u", "/"} {
		next, _ := m.Update(key(k))
		got := next.(Model)
		if got.mode != "" {
			t.Fatalf("help open: %q must not enter mode %q", k, got.mode)
		}
		if got.view != m.view {
			t.Fatalf("help open: %q must not change view", k)
		}
		if !got.showHelp {
			t.Fatalf("help open: %q must not close help", k)
		}
	}
}

// TestHelpDismissKeys checks the three documented ways to close the panel, and
// that none of them leak into navigation.
func TestHelpDismissKeys(t *testing.T) {
	for _, k := range []string{"?", "esc", "q"} {
		m := newTestModel()
		m = sendList(t, m)
		m = apply(t, m, key("?"))
		if !m.showHelp {
			t.Fatalf("%q setup: help must be open", k)
		}
		next, _ := m.Update(key(k))
		got := next.(Model)
		if got.showHelp {
			t.Fatalf("%q must close help", k)
		}
		if got.quitting {
			t.Fatalf("%q must close help, not quit", k)
		}
		if got.view != ViewTunnels {
			t.Fatalf("%q must leave the view unchanged, got %v", k, got.view)
		}
	}
}

// TestHelpOpenableFromEveryView stops help being reachable in one view only.
func TestHelpOpenableFromEveryView(t *testing.T) {
	f := &fakeBackend{tunnels: []cf.Tunnel{{ID: "t1", Name: "app"}}}
	m := New(Backends{Local: f})
	m = sendList(t, m)

	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil {
		m = apply(t, m, cmd())
	}
	m = apply(t, m, key("?"))
	if !m.showHelp {
		t.Fatal("? must open help from the routes view")
	}
	if !strings.Contains(m.View(), "esc          back to the tunnel list") {
		t.Fatal("routes help must describe the routes keys")
	}

	// And from the form.
	m = apply(t, m, key("?")) // close
	m = apply(t, m, key("a"))
	if m.view != ViewForm {
		t.Fatalf("a should open the form, view=%v", m.view)
	}
	m = apply(t, m, key("?"))
	if !m.showHelp {
		t.Fatal("? must open help from the add-route form")
	}
	if !strings.Contains(m.View(), "add-route form:") {
		t.Fatal("form help must describe the form keys")
	}
}

// filterModel builds a model with three routes and a filter typed in, matching
// what the user sees mid-filter.
func filterModel(t *testing.T, query string) Model {
	t.Helper()
	f := &fakeBackend{
		tunnels: []cf.Tunnel{{ID: "t1", Name: "app"}},
		routes: map[string][]cf.Route{"t1": {
			{Hostname: "a.example.com", Service: "http://localhost:1"},
			{Hostname: "b.example.com", Service: "http://localhost:2"},
			{Service: "http_status:404"},
		}},
	}
	m := New(Backends{Local: f})
	m = sendList(t, m)
	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil {
		m = apply(t, m, cmd())
	}
	m = apply(t, m, key("/"))
	for _, ch := range query {
		m = apply(t, m, key(string(ch)))
	}
	return m
}

// TestFilterAnchorsSelection is the regression guard for a filter/selection
// mismatch: the list showed the filtered routes while routeAt still pointed at a
// hidden one, so 'o' opened the wrong URL and J/K reordered invisible rows.
func TestFilterAnchorsSelection(t *testing.T) {
	m := filterModel(t, "b.example")
	m = apply(t, m, key("enter"))

	if got := m.routes[m.routeAt].Hostname; got != "b.example.com" {
		t.Fatalf("after filtering, selection is %q, want b.example.com", got)
	}
	if pos := m.visiblePos(m.routeAt); pos < 0 {
		t.Fatal("selection must be a visible row")
	}
}

// TestFilterOpenOpensVisibleRoute checks 'o' acts on the highlighted row.
func TestFilterOpenOpensVisibleRoute(t *testing.T) {
	m := filterModel(t, "b.example")
	m = apply(t, m, key("enter"))
	m = apply(t, m, key("o"))

	if !strings.Contains(m.status, "b.example.com") {
		t.Fatalf("o must open the visible route, status=%q", m.status)
	}
}

// TestFilterReorderStaysVisible stops J/K from reordering a filtered-out row.
func TestFilterReorderStaysVisible(t *testing.T) {
	m := filterModel(t, "b.example")
	m = apply(t, m, key("enter"))

	before := []string{m.routes[0].Hostname, m.routes[1].Hostname}
	m = apply(t, m, key("J")) // b is the last visible routable row: must not move past it
	after := []string{m.routes[0].Hostname, m.routes[1].Hostname}
	if before[0] != after[0] || before[1] != after[1] {
		t.Fatalf("J must not reorder hidden rows: before=%v after=%v", before, after)
	}
	if got := m.routes[m.routeAt].Hostname; got != "b.example.com" {
		t.Fatalf("selection drifted to %q", got)
	}
}

// TestFilterNavigationStaysInVisibleList walks up/down under a filter and checks
// the cursor never lands on a filtered-out route.
func TestFilterNavigationStaysInVisibleList(t *testing.T) {
	m := filterModel(t, "b.example")
	m = apply(t, m, key("enter"))

	for _, k := range []string{"down", "up", "down", "up"} {
		m = apply(t, m, key(k))
		if m.visiblePos(m.routeAt) < 0 {
			t.Fatalf("after %q the selection is a hidden row (%q)", k, m.routes[m.routeAt].Hostname)
		}
	}
}

// TestFilterClearKeepsSelectionUsable checks that clearing the filter leaves a
// valid selection rather than a stale index into the old visible list.
func TestFilterClearKeepsSelectionUsable(t *testing.T) {
	m := filterModel(t, "b.example")
	m = apply(t, m, key("enter"))
	m = apply(t, m, key("/"))
	m = apply(t, m, key("esc"))

	if m.filter != "" {
		t.Fatalf("esc must clear the filter, got %q", m.filter)
	}
	if m.routeAt < 0 || m.routeAt >= len(m.routes) {
		t.Fatalf("selection out of range: %d of %d", m.routeAt, len(m.routes))
	}
}

// withTokenFetch swaps the token fetcher for the duration of a test and restores
// it afterwards, so no real cloudflared process is ever spawned.
func withTokenFetch(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	prev := tokenFetcher
	tokenFetcher = fn
	t.Cleanup(func() { tokenFetcher = prev })
}

// openToken presses t on the tunnel list and delivers the async fetch result.
func openToken(t *testing.T, m Model, k string) Model {
	t.Helper()
	next, cmd := m.Update(key(k))
	m = next.(Model)
	if cmd != nil {
		m = apply(t, m, cmd())
	}
	return m
}

// TestTokenFetchShowsMaskedByDefault checks that t fetches the connector token
// but never reveals it until the user asks.
func TestTokenFetchShowsMaskedByDefault(t *testing.T) {
	withTokenFetch(t, func(string) (string, error) { return "SECRET-TOKEN-abcdef123456", nil })
	m := newCLIModel()
	m = sendList(t, m)
	m = openToken(t, m, "t")

	if m.tokenTunnel != "app-tunnel" {
		t.Fatalf("t must target the selected tunnel, got %q", m.tokenTunnel)
	}
	if m.token != "SECRET-TOKEN-abcdef123456" {
		t.Fatalf("token not stored, got %q", m.token)
	}
	view := m.View()
	if strings.Contains(view, "SECRET-TOKEN-abcdef123456") {
		t.Fatal("token must be masked until revealed")
	}
	if !strings.Contains(view, "connector token for app-tunnel") {
		t.Fatalf("panel must name the tunnel, got %q", view)
	}
}

// TestTokenRevealToggle checks the v key reveals and hides the token.
func TestTokenRevealToggle(t *testing.T) {
	withTokenFetch(t, func(string) (string, error) { return "SECRET-TOKEN-abcdef123456", nil })
	m := newCLIModel()
	m = sendList(t, m)
	m = openToken(t, m, "t")

	m = apply(t, m, key("v"))
	if !strings.Contains(m.View(), "SECRET-TOKEN-abcdef123456") {
		t.Fatal("v must reveal the token")
	}
	m = apply(t, m, key("v"))
	if strings.Contains(m.View(), "SECRET-TOKEN-abcdef123456") {
		t.Fatal("v must hide the token again")
	}
}

// TestTokenPanelIsModal stops the panel leaking keys to the tunnel list.
func TestTokenPanelIsModal(t *testing.T) {
	withTokenFetch(t, func(string) (string, error) { return "tok", nil })
	m := newCLIModel()
	m = sendList(t, m)
	m = openToken(t, m, "t")

	for _, k := range []string{"n", "d", "enter"} {
		next, _ := m.Update(key(k))
		got := next.(Model)
		if got.mode != "" {
			t.Fatalf("token panel open: %q must not enter mode %q", k, got.mode)
		}
		if got.view != ViewTunnels {
			t.Fatalf("token panel open: %q must not change view", k)
		}
		if got.tokenTunnel == "" {
			t.Fatalf("token panel open: %q must not close the panel", k)
		}
	}
}

// TestTokenPanelCloseClearsSecret checks every documented close key drops the
// credential from the model instead of leaving it in memory.
func TestTokenPanelCloseClearsSecret(t *testing.T) {
	for _, k := range []string{"t", "esc", "q"} {
		withTokenFetch(t, func(string) (string, error) { return "SECRET-TOKEN-abcdef123456", nil })
		m := newCLIModel()
		m = sendList(t, m)
		m = openToken(t, m, "t")

		next, _ := m.Update(key(k))
		got := next.(Model)
		if got.tokenTunnel != "" || got.token != "" {
			t.Fatalf("%q must clear the token, got tunnel=%q token=%q", k, got.tokenTunnel, got.token)
		}
		if got.quitting {
			t.Fatalf("%q must close the panel, not quit", k)
		}
	}
}

// TestTokenErrorIsShown ensures a failed fetch reports the reason instead of a
// blank or stale panel.
func TestTokenErrorIsShown(t *testing.T) {
	withTokenFetch(t, func(string) (string, error) {
		return "", errors.New("boom")
	})
	m := newCLIModel()
	m = sendList(t, m)
	m = openToken(t, m, "t")

	if m.tokenErr == "" {
		t.Fatal("fetch failure must be reported")
	}
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("panel must show the error, got %q", m.View())
	}
}

// TestTokenPrefersAPIOverCLI checks the t action uses the backend's API lookup
// when a tokenizer exists, so the CLI is never shelled out to.
func TestTokenPrefersAPIOverCLI(t *testing.T) {
	called := false
	withTokenFetch(t, func(string) (string, error) {
		called = true
		return "cli-token", nil
	})
	f := &fakeBackend{
		tunnels: []cf.Tunnel{{ID: "app-id-1", Name: "app-tunnel", Status: "healthy"}},
		token:   "api-token",
	}
	m := New(Backends{Remote: f, Local: f})
	m = sendList(t, m)
	m = openToken(t, m, "t")

	if called {
		t.Fatal("CLI fetcher must not run when the backend can fetch the token")
	}
	if len(f.tokenIDs) != 1 || f.tokenIDs[0] != "app-id-1" {
		t.Fatalf("backend must be asked by tunnel ID, got %v", f.tokenIDs)
	}
	if m.token != "api-token" {
		t.Fatalf("token = %q, want api-token", m.token)
	}
}

// TestTokenFallsBackToCLI checks a backend without an API tokenizer still gets
// the token, via the cloudflared CLI path.
func TestTokenFallsBackToCLI(t *testing.T) {
	withTokenFetch(t, func(name string) (string, error) { return "cli-token-" + name, nil })
	base := &fakeBackend{tunnels: []cf.Tunnel{{ID: "app-id-1", Name: "app-tunnel", Status: "healthy"}}}
	m := New(Backends{Local: plainBackend{base: base}})
	m = sendList(t, m)
	m = openToken(t, m, "t")

	if m.token != "cli-token-app-tunnel" {
		t.Fatalf("token = %q, want the CLI fallback value", m.token)
	}
	if len(base.tokenIDs) != 0 {
		t.Fatalf("API path must not run without a tokenizer, got %v", base.tokenIDs)
	}
}

// TestTokenizerForPrefersRemote pins the backend selection order.
func TestTokenizerForPrefersRemote(t *testing.T) {
	remote := &fakeBackend{}
	local := &fakeBackend{}
	if got, ok := (Backends{Remote: remote, Local: local}).tokenizerFor(); !ok || got != tunnelTokenizer(remote) {
		t.Fatalf("tokenizerFor must prefer Remote, got %v ok=%v", got, ok)
	}
	if got, ok := (Backends{Local: local}).tokenizerFor(); !ok || got != tunnelTokenizer(local) {
		t.Fatalf("tokenizerFor must fall back to Local, got %v ok=%v", got, ok)
	}
	if _, ok := (Backends{}).tokenizerFor(); ok {
		t.Fatal("tokenizerFor must report false when no backend can fetch tokens")
	}
}

// TestRoutesViewEmptyDoesNotPanic covers the render path when a tunnel has no
// ingress at all, including while a filter is typed in that matches nothing.
func TestRoutesViewEmptyDoesNotPanic(t *testing.T) {
	f := &fakeBackend{tunnels: []cf.Tunnel{{ID: "t1", Name: "empty-tunnel"}}}
	m := New(Backends{Local: f})
	m = sendList(t, m)

	next, cmd := m.Update(key("enter"))
	m = next.(Model)
	if cmd != nil {
		m = apply(t, m, cmd())
	}
	if m.view != ViewRoutes {
		t.Fatalf("enter should open routes, view=%v", m.view)
	}

	out := m.View()
	if !strings.Contains(out, "no routes") {
		t.Fatalf("empty route list must say so, got:\n%s", out)
	}

	// Typing a filter over an empty list must also stay safe.
	m = apply(t, m, key("/"))
	for _, ch := range "zzz" {
		m = apply(t, m, key(string(ch)))
	}
	if out := m.View(); !strings.Contains(out, "no routes") {
		t.Fatalf("filtered empty route list must stay safe, got:\n%s", out)
	}

	// Movement keys over an empty list must not panic either.
	for _, k := range []string{"down", "up", "J", "K"} {
		m = apply(t, m, key(k))
	}
	_ = m.View()
}
