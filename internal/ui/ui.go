// Package ui renders cftui's bubbletea interface: the tunnel list, the route
// table for the selected tunnel, a detail pane, and the port-first add-route
// form that backs the "manage self port" flow.
package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/masfana/cftui/internal/cf"
	"github.com/masfana/cftui/internal/cred"
	"github.com/masfana/cftui/internal/health"
	"github.com/masfana/cftui/internal/local"
	"github.com/masfana/cftui/internal/validate"
)

// View is the screen currently taking keystrokes.
type View int

const (
	// ViewTunnels is the tunnel list, the entry screen.
	ViewTunnels View = iota
	// ViewRoutes is the route table plus detail pane for one tunnel.
	ViewRoutes
	// ViewForm is the add-route form. It opens with the local port field
	// focused, which is the "manage self port" fast path.
	ViewForm
)

// Backends bundles the two data sources the UI drives.
type Backends struct {
	Remote cf.Backend
	Local  cf.Backend
}

// Detect tells the UI which side owns each tunnel's configuration, so the
// tunnel list can badge it and the routes view can warn when the two sides
// disagree (FR-20 / FR-20a). Every field is optional: with none set the UI
// simply shows no badges and no disagreement warning.
type Detect struct {
	// Management maps a tunnel name to its owning side. A missing name is
	// reported as "unknown".
	Management map[string]local.Management
	// LocalRules returns the locally configured ingress rules for a tunnel, the
	// left-hand side of the disagreement comparison.
	LocalRules func(tunnel string) []local.Rule
	// RemoteHosts returns the hostnames Cloudflare currently holds for a tunnel,
	// the right-hand side of the comparison. A nil function skips the check.
	RemoteHosts func(ctx context.Context, tunnel string) ([]string, error)
}

type tunnelAdmin interface {
	CreateTunnel(ctx context.Context, name string) (cf.Tunnel, error)
	DeleteTunnel(ctx context.Context, tunnelID string) error
}

// tunnelTokenizer fetches a tunnel's connector token. The remote backend serves
// it from the API by tunnel ID; the local backend falls back to the cloudflared
// CLI, which resolves the name itself.
type tunnelTokenizer interface {
	TunnelToken(ctx context.Context, tunnelID string) (string, error)
}

// tokenizerFor prefers the remote backend, whose API lookup is keyed by tunnel
// ID and needs no local cloudflared, and falls back to the local backend.
func (b Backends) tokenizerFor() (tunnelTokenizer, bool) {
	if t, ok := b.Remote.(tunnelTokenizer); ok {
		return t, true
	}
	if t, ok := b.Local.(tunnelTokenizer); ok {
		return t, true
	}
	return nil, false
}

// tunnelAdminFor prefers the remote backend, which creates tunnels through the
// Cloudflare API, and falls back to the local backend's CLI-backed
// implementation. It returns false when neither backend can create tunnels.
func (b Backends) tunnelAdminFor() (tunnelAdmin, bool) {
	if a, ok := b.Remote.(tunnelAdmin); ok {
		return a, true
	}
	if a, ok := b.Local.(tunnelAdmin); ok {
		return a, true
	}
	return nil, false
}

// readBackend is the backend the UI reads tunnels and routes from. The remote
// backend wins when present, because it reflects what Cloudflare actually holds
// and works for tunnels the local config.yml never mentions. The local file
// backend is the offline fallback.
func (b Backends) readBackend() cf.Backend {
	if b.Remote != nil {
		return b.Remote
	}
	return b.Local
}

// Model is the bubbletea model.
type Model struct {
	backends Backends
	detect   Detect

	view   View
	width  int
	height int

	tunnels []cf.Tunnel
	cursor  int

	tunnel    cf.Tunnel
	routes    []cf.Route
	committed []cf.Route
	routeAt   int

	form        Form
	probe       health.Result
	probed      bool
	status      string
	errText     string
	mode        string
	confirmName string
	quitWarned  bool

	filter      string
	filtering   bool
	showHelp    bool
	lastOpenURL string
	disagree    string

	// token holds the connector token for m.tunnel once fetched by the t key.
	// It is a live credential, so it stays masked until the user reveals it and
	// is cleared whenever the selection or view changes.
	token        string
	tokenTunnel  string
	tokenErr     string
	tokenShown   bool
	tokenLoading bool

	quitting bool
}

// RouteURL renders the public https URL a hosted route answers on. A route
// with no hostname (the catch-all) has no URL, so it returns "".
func RouteURL(r cf.Route) string {
	host := strings.TrimSpace(r.Hostname)
	if host == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(r.Service), "http_status:404") {
		return ""
	}
	path := strings.TrimSpace(r.Path)
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "https://" + host + path
}

// FilterRoutes keeps the routes whose hostname, path or service contains the
// query, always retaining the catch-all so the list never looks empty. An empty
// query returns the input unchanged.
func FilterRoutes(routes []cf.Route, query string) []cf.Route {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return routes
	}
	out := make([]cf.Route, 0, len(routes))
	for _, r := range routes {
		if cf.IsCatchAll(r) || strings.Contains(strings.ToLower(r.Hostname+r.Path+r.Service), q) {
			out = append(out, r)
		}
	}
	return out
}

// Form holds the add-route form's editable state.
type Form struct {
	Port     string
	Hostname string
	Service  string
	Focus    int
}

// formFields lists the form's fields in tab order. Port is first so the form
// opens ready for the self-port flow.
var formFields = []string{"port", "hostname", "service"}

// New builds the starting model.
func New(b Backends) Model {
	return NewWithDetect(b, Detect{})
}

// NewWithDetect builds the starting model with tunnel-management detection
// wired in. New is the detection-free shorthand.
func NewWithDetect(b Backends, d Detect) Model {
	return Model{
		backends: b,
		detect:   d,
		view:     ViewTunnels,
		form:     Form{Service: "http", Focus: 0},
	}
}

// Management returns the owning side of a tunnel, defaulting to "unknown" when
// detection has nothing to say about it.
func (m Model) Management(name string) local.Management {
	if v, ok := m.detect.Management[name]; ok && v != "" {
		return v
	}
	return local.ManagedUnknown
}

// badge renders a tunnel's owning side as a short, fixed-width tag. A remote
// tunnel is called out because edits there bypass the local config.yml.
func (m Model) badge(name string) string {
	switch m.Management(name) {
	case local.ManagedLocal:
		return "MANUAL"
	case local.ManagedRemote:
		return "MANAGED"
	default:
		return "UNKNOWN"
	}
}

// tunnelListMsg carries a refreshed tunnel list.
type tunnelListMsg struct {
	tunnels []cf.Tunnel
	err     error
}

// routeListMsg carries a refreshed route list for one tunnel.
type routeListMsg struct {
	tunnel cf.Tunnel
	routes []cf.Route
	err    error
}

// probeMsg carries the outcome of a live TCP probe on the typed port.
type probeMsg struct {
	port string
	res  health.Result
}

type tunnelCreatedMsg struct{ err error }
type tunnelDeletedMsg struct{ err error }
type routesAppliedMsg struct{ err error }

// tunnelTokenMsg carries the result of fetching a tunnel's connector token.
type tunnelTokenMsg struct {
	name  string
	token string
	err   error
}

// tokenFetcher is swapped in tests so no real cloudflared process runs. It is a
// package variable because the tunnel list must fetch the token asynchronously.
var tokenFetcher = func(name string) (string, error) { return cred.TunnelToken(context.Background(), name) }

func tokenFor(name string) tunnelTokenMsg {
	token, err := tokenFetcher(name)
	return tunnelTokenMsg{name: name, token: token, err: err}
}

// Init loads the tunnel list on start.
func (m Model) Init() tea.Cmd {
	return refreshTunnels(m.backends.readBackend())
}

// refreshTunnels reads the tunnel list from the given backend.
func refreshTunnels(b cf.Backend) tea.Cmd {
	return func() tea.Msg {
		if b == nil {
			return tunnelListMsg{err: fmt.Errorf("no backend configured")}
		}
		list, err := b.ListTunnels(context.Background())
		return tunnelListMsg{tunnels: list, err: err}
	}
}

// refreshRoutes reads one tunnel's routes.
func refreshRoutes(b cf.Backend, t cf.Tunnel) tea.Cmd {
	return func() tea.Msg {
		if b == nil {
			return routeListMsg{tunnel: t, err: fmt.Errorf("no backend configured")}
		}
		routes, err := b.GetRoutes(context.Background(), t.ID)
		return routeListMsg{tunnel: t, routes: routes, err: err}
	}
}

// probePort runs a live TCP probe so the form can warn before commit.
func probePort(port string) tea.Cmd {
	return func() tea.Msg {
		n, err := parsePort(port)
		if err != nil {
			return probeMsg{port: port}
		}
		return probeMsg{port: port, res: health.Probe(context.Background(), "localhost", n)}
	}
}

// clearToken drops the fetched connector token and any reveal state. Called
// whenever the token panel closes so the secret does not linger in memory.
func (m *Model) clearToken() {
	m.token = ""
	m.tokenTunnel = ""
	m.tokenErr = ""
	m.tokenShown = false
	m.tokenLoading = false
}

// Update advances the model in response to a message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case openURLMsg:
		if msg.err != nil {
			m.errText = "could not open " + msg.url + ": " + msg.err.Error()
			return m, nil
		}
		m.errText = ""
		m.status = "opened " + msg.url
		return m, nil

	case disagreeMsg:
		if msg.err != nil || msg.name != m.tunnel.Name {
			m.disagree = ""
			return m, nil
		}
		m.disagree = local.Disagreement(msg.name, msg.localRules, msg.remoteHosts)
		return m, nil

	case tunnelTokenMsg:
		if msg.name != m.tokenTunnel {
			return m, nil
		}
		m.tokenLoading = false
		if msg.err != nil {
			m.tokenErr = msg.err.Error()
			m.token = ""
			return m, nil
		}
		m.tokenErr = ""
		m.token = msg.token
		return m, nil

	case tunnelListMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.errText = ""
		m.tunnels = msg.tunnels
		if m.cursor >= len(m.tunnels) {
			m.cursor = max(0, len(m.tunnels)-1)
		}
		return m, nil

	case routeListMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.errText = ""
		m.tunnel = msg.tunnel
		// EnsureCatchAllLast normalizes the trailing catch-all, which is what the
		// write path requires. It must not run on an empty list: a tunnel with no
		// ingress has no catch-all, and synthesizing one here would show a route
		// the account does not hold and stage it as a pending change.
		if len(msg.routes) == 0 {
			m.routes = nil
		} else {
			m.routes = cf.EnsureCatchAllLast(msg.routes)
		}
		m.committed = append([]cf.Route(nil), m.routes...)
		m.routeAt = 0
		return m, m.refreshDisagreement()
	case tunnelCreatedMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.mode = ""
		m.status = "tunnel created"
		return m, refreshTunnels(m.backends.readBackend())
	case tunnelDeletedMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.mode = ""
		m.confirmName = ""
		m.status = "tunnel deleted"
		return m, refreshTunnels(m.backends.readBackend())
	case routesAppliedMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.status = "routes applied"
		return m, refreshRoutes(m.backends.readBackend(), m.tunnel)

	case probeMsg:
		if msg.port != m.form.Port {
			return m, nil
		}
		m.probe, m.probed = msg.res, true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey dispatches a keystroke for the active view.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		if Changed(m.committed, m.routes) && !m.quitWarned {
			m.quitWarned = true
			m.errText = "unsaved changes; press ctrl+c again to quit"
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	}

	if m.showHelp {
		switch msg.String() {
		case "?", "esc", "q":
			m.showHelp = false
			m.errText = ""
		}
		return m, nil
	}

	if m.tokenTunnel != "" {
		switch msg.String() {
		case "t", "esc", "q":
			m.clearToken()
		case "v":
			m.tokenShown = !m.tokenShown
		}
		return m, nil
	}

	if msg.String() == "?" {
		m.showHelp = true
		return m, nil
	}

	switch m.view {
	case ViewForm:
		return m.handleFormKey(msg)
	case ViewRoutes:
		return m.handleRouteKey(msg)
	default:
		return m.handleTunnelKey(msg)
	}
}

// handleTunnelKey drives the tunnel list.
func (m Model) handleTunnelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == "create-tunnel" {
		return m.handleTunnelCreate(msg)
	}
	if m.mode == "delete-tunnel" {
		return m.handleTunnelDelete(msg)
	}
	switch msg.String() {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.tunnels)-1 {
			m.cursor++
		}
	case "r":
		return m, refreshTunnels(m.backends.readBackend())
	case "n":
		m.mode, m.confirmName, m.errText = "create-tunnel", "", ""
	case "a":
		if len(m.tunnels) == 0 {
			m.errText = "create or select a tunnel first"
			return m, nil
		}
		m.tunnel = m.tunnels[m.cursor]
		m.view = ViewForm
		m.form.Focus = 0
		m.probed = false
		m.status = ""
		return m, refreshRoutes(m.backends.readBackend(), m.tunnel)
	case "d":
		if len(m.tunnels) == 0 {
			return m, nil
		}
		m.mode, m.confirmName, m.errText = "delete-tunnel", "", ""
	case "t":
		if len(m.tunnels) == 0 {
			m.errText = "select a tunnel first"
			return m, nil
		}
		if m.cursor >= len(m.tunnels) {
			m.errText = "no tunnel selected"
			return m, nil
		}
		t := m.tunnels[m.cursor]
		m.tokenTunnel = t.Name
		m.tokenErr = ""
		m.tokenShown = false
		m.tokenLoading = true
		id, name := t.ID, t.Name
		tok, ok := m.backends.tokenizerFor()
		if !ok {
			return m, func() tea.Msg { return tokenFor(name) }
		}
		return m, func() tea.Msg {
			token, err := tok.TunnelToken(context.Background(), id)
			return tunnelTokenMsg{name: name, token: token, err: err}
		}
	case "enter":
		if len(m.tunnels) == 0 {
			return m, nil
		}
		t := m.tunnels[m.cursor]
		m.view = ViewRoutes
		return m, refreshRoutes(m.backends.readBackend(), t)
	}
	return m, nil
}

func (m Model) handleTunnelCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ""
		return m, nil
	case "backspace":
		if len(m.confirmName) > 0 {
			m.confirmName = m.confirmName[:len(m.confirmName)-1]
		}
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.confirmName)
		if !validTunnelName(name) {
			m.errText = "tunnel name must be 1-63 lowercase letters, digits, or hyphens"
			return m, nil
		}
		admin, ok := m.backends.tunnelAdminFor()
		if !ok {
			m.errText = "backend does not support tunnel creation"
			return m, nil
		}
		return m, func() tea.Msg { _, err := admin.CreateTunnel(context.Background(), name); return tunnelCreatedMsg{err} }
	}
	if len(msg.Runes) > 0 {
		m.confirmName += string(msg.Runes)
	}
	return m, nil
}

func (m Model) handleTunnelDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode, m.confirmName = "", ""
		return m, nil
	case "backspace":
		if len(m.confirmName) > 0 {
			m.confirmName = m.confirmName[:len(m.confirmName)-1]
		}
		return m, nil
	case "enter":
		if m.cursor >= len(m.tunnels) {
			m.errText = "no tunnel selected"
			return m, nil
		}
		if strings.TrimSpace(m.confirmName) != m.tunnels[m.cursor].Name {
			m.errText = "type the exact tunnel name to delete"
			return m, nil
		}
		admin, ok := m.backends.tunnelAdminFor()
		if !ok {
			m.errText = "backend does not support tunnel deletion"
			return m, nil
		}
		id := m.tunnels[m.cursor].ID
		return m, func() tea.Msg { return tunnelDeletedMsg{admin.DeleteTunnel(context.Background(), id)} }
	}
	if len(msg.Runes) > 0 {
		m.confirmName += string(msg.Runes)
	}
	return m, nil
}

// anchorRouteAt points routeAt at a currently visible route so the selection and
// the drawn list never disagree. It prefers the route already selected; if that
// one is filtered out it falls back to the first visible row.
func (m *Model) anchorRouteAt() {
	if len(m.routes) == 0 {
		m.routeAt = 0
		return
	}
	if m.visiblePos(m.routeAt) >= 0 {
		return
	}
	if idx := m.realIndex(0); idx >= 0 {
		m.routeAt = idx
		return
	}
	m.routeAt = 0
}

// handleRouteKey drives the route table.
func (m Model) handleRouteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		if msg.String() == "esc" {
			m.filtering = false
			m.filter = ""
			m.anchorRouteAt()
			return m, nil
		}
		if msg.String() == "enter" {
			m.filtering = false
			return m, nil
		}
		if msg.String() == "backspace" {
			if m.filter != "" {
				m.filter = m.filter[:len(m.filter)-1]
			}
			m.anchorRouteAt()
			return m, nil
		}
		if len(msg.Runes) > 0 {
			m.filter += string(msg.Runes)
			m.anchorRouteAt()
		}
		return m, nil
	}
	if m.mode == "discard" {
		if msg.String() == "esc" {
			m.mode = ""
		} else if msg.String() == "y" {
			m.routes = append([]cf.Route(nil), m.committed...)
			m.mode = ""
			m.status = "staged routes discarded"
		}
		return m, nil
	}
	switch msg.String() {
	case "o":
		if m.routeAt < len(m.routes) {
			url := RouteURL(m.routes[m.routeAt])
			if url == "" {
				m.errText = "this route has no public URL"
				return m, nil
			}
			m.lastOpenURL = url
			m.status = "opening " + url
			return m, openURL(url)
		}
		return m, nil
	case "/":
		m.filtering = true
		m.filter = ""
		return m, nil
	case "q":
		if Changed(m.committed, m.routes) && !m.quitWarned {
			m.quitWarned = true
			m.errText = "unsaved changes; press q again to quit"
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.view = ViewTunnels
		m.errText = ""
		m.status = ""
		return m, nil
	case "a":
		m.view = ViewForm
		m.form.Focus = 0
		m.probed = false
		return m, nil
	case "r":
		return m, refreshRoutes(m.backends.readBackend(), m.tunnel)
	case "w":
		if !Changed(m.committed, m.routes) {
			m.status = "no staged changes"
			return m, nil
		}
		routes := append([]cf.Route(nil), m.routes...)
		backend := m.backends.readBackend()
		id := m.tunnel.ID
		return m, func() tea.Msg { return routesAppliedMsg{backend.PutRoutes(context.Background(), id, routes)} }
	case "u":
		if Changed(m.committed, m.routes) {
			m.mode = "discard"
		}
	case "J":
		m.moveRoute(1)
	case "K":
		m.moveRoute(-1)
	case "p":
		if m.routeAt < len(m.routes) && !cf.IsCatchAll(m.routes[m.routeAt]) {
			r := m.routes[m.routeAt]
			copy(m.routes[1:m.routeAt+1], m.routes[:m.routeAt])
			m.routes[0] = r
			m.routeAt = 0
			m.routes = cf.EnsureCatchAllLast(m.routes)
		}
	case "up", "k":
		if m.filter != "" {
			if pos := m.visiblePos(m.routeAt); pos > 0 {
				m.routeAt = m.realIndex(pos - 1)
			}
		} else if m.routeAt > 0 {
			m.routeAt--
		}
	case "down", "j":
		if m.filter != "" {
			if pos := m.visiblePos(m.routeAt); pos >= 0 && pos < len(m.visibleRoutes())-1 {
				m.routeAt = m.realIndex(pos + 1)
			}
		} else if m.routeAt < len(m.routes)-1 {
			m.routeAt++
		}
	}
	return m, nil
}

// moveRoute moves the selected route one slot through the list it is shown in.
// With a filter active only visible neighbours are considered, so J/K never
// silently reorder a row the user cannot see.
func (m *Model) moveRoute(delta int) {
	to := m.routeAt + delta
	if m.filter != "" {
		pos := m.visiblePos(m.routeAt)
		if pos < 0 {
			return
		}
		to = m.realIndex(pos + delta)
	}
	if m.routeAt < 0 || m.routeAt >= len(m.routes) || cf.IsCatchAll(m.routes[m.routeAt]) || to < 0 || to >= len(m.routes) || cf.IsCatchAll(m.routes[to]) {
		return
	}
	m.routes[m.routeAt], m.routes[to] = m.routes[to], m.routes[m.routeAt]
	m.routeAt = to
	m.routes = cf.EnsureCatchAllLast(m.routes)
}

// handleFormKey drives the add-route form, including the live port probe.
func (m Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.tunnel.ID != "" {
			m.view = ViewRoutes
			return m, nil
		}
		m.view = ViewTunnels
		return m, nil
	case "tab", "down":
		m.form.Focus = (m.form.Focus + 1) % len(formFields)
		return m, nil
	case "shift+tab", "up":
		m.form.Focus = (m.form.Focus + len(formFields) - 1) % len(formFields)
		return m, nil
	case "backspace":
		m.editField(func(s string) string {
			if s == "" {
				return s
			}
			return s[:len(s)-1]
		})
		if m.fieldName() == "port" {
			return m, m.reprobe()
		}
		return m, nil
	case "enter":
		return m.commit()
	}

	if len(msg.Runes) > 0 {
		m.editField(func(s string) string { return s + string(msg.Runes) })
		if m.fieldName() == "port" {
			return m, m.reprobe()
		}
	}
	return m, nil
}

// reprobe asks for a fresh probe on the current port text, if it parses.
func (m *Model) reprobe() tea.Cmd {
	m.probed = false
	port := strings.TrimSpace(m.form.Port)
	if port == "" {
		return nil
	}
	if _, err := parsePort(port); err != nil {
		return nil
	}
	return probePort(port)
}

// fieldName reports the key of the focused form field.
func (m *Model) fieldName() string { return formFields[m.form.Focus] }

// editField applies fn to the focused field's value.
func (m *Model) editField(fn func(string) string) {
	switch m.fieldName() {
	case "port":
		m.form.Port = fn(m.form.Port)
	case "hostname":
		m.form.Hostname = fn(m.form.Hostname)
	case "service":
		m.form.Service = fn(m.form.Service)
	}
}

// commit validates the form and stages the change, refusing a malformed port.
func (m Model) commit() (tea.Model, tea.Cmd) {
	if m.tunnel.ID == "" {
		m.errText = "select a tunnel before adding a route"
		m.view = ViewTunnels
		return m, nil
	}
	port, err := parsePort(strings.TrimSpace(m.form.Port))
	if err != nil {
		m.errText = err.Error()
		return m, nil
	}
	service := strings.TrimSpace(m.form.Service)
	if service == "" {
		service = "http"
	}
	route := cf.Route{
		Hostname: strings.TrimSpace(m.form.Hostname),
		Service:  fmt.Sprintf("%s://localhost:%d", service, port),
	}
	for _, existing := range m.routes {
		if strings.EqualFold(existing.Hostname, route.Hostname) && existing.Path == route.Path {
			m.errText = fmt.Sprintf("%s%s is already routed on this tunnel", route.Hostname, route.Path)
			return m, nil
		}
	}

	routes := append([]cf.Route(nil), m.routes...)
	if len(routes) > 1 {
		routes = append(routes[:len(routes)-1], route, routes[len(routes)-1])
	} else {
		routes = append(routes, route)
	}
	m.routes = cf.EnsureCatchAllLast(routes)
	m.errText = ""
	m.status = fmt.Sprintf("staged %s -> %s", route.Hostname, route.Service)
	m.view = ViewRoutes
	return m, nil
}

// View renders the active screen.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	switch m.view {
	case ViewTunnels:
		b.WriteString(m.viewTunnels())
	case ViewRoutes:
		b.WriteString(m.viewRoutes())
	case ViewForm:
		b.WriteString(m.viewForm())
	}
	if m.errText != "" {
		b.WriteString("\n" + errStyle.Render("error: "+m.errText))
	} else if m.status != "" {
		b.WriteString("\n" + okStyle.Render(m.status))
	}
	if !m.showHelp {
		b.WriteString("\n" + helpStyle.Render(m.helpLine()))
	}
	return b.String()
}

// helpLine returns the key hints for the active view.
func (m Model) helpLine() string {
	if m.filtering {
		return "type to filter, enter to keep, esc to clear"
	}
	switch m.view {
	case ViewForm:
		return "type the local port, tab to move, enter to stage, esc to cancel"
	case ViewRoutes:
		return "a add  o open  J/K reorder  p pin  w apply  u discard  / filter  ? help  r refresh  esc back  q quit"
	default:
		return "n new tunnel  d delete tunnel  t token  enter open  ? help  r refresh  q quit"
	}
}

// visibleRoutes is what the routes view actually draws: the filtered list when a
// filter is active, otherwise every route. Selection must always be expressed
// against this list, or the cursor and the screen disagree.
func (m Model) visibleRoutes() []cf.Route {
	return FilterRoutes(m.routes, m.filter)
}

// realIndex maps a position in the visible list back to an index in m.routes. It
// returns -1 when the position is not visible. Matching is by value so it works
// even while a route is being reordered.
func (m Model) realIndex(visiblePos int) int {
	vis := m.visibleRoutes()
	if visiblePos < 0 || visiblePos >= len(vis) {
		return -1
	}
	target := vis[visiblePos]
	for i, r := range m.routes {
		if r.Hostname == target.Hostname && r.Path == target.Path && r.Service == target.Service {
			return i
		}
	}
	return -1
}

// visiblePos maps an index in m.routes to its position in the visible list, or
// -1 when the route is currently filtered out.
func (m Model) visiblePos(realIdx int) int {
	if realIdx < 0 || realIdx >= len(m.routes) {
		return -1
	}
	target := m.routes[realIdx]
	for i, r := range m.visibleRoutes() {
		if r.Hostname == target.Hostname && r.Path == target.Path && r.Service == target.Service {
			return i
		}
	}
	return -1
}

// for the view you are actually in, then the global ones, and always ends with
// how to dismiss the panel.
func helpText(view View) string {
	lines := []string{"cftui key reference", ""}
	switch view {
	case ViewRoutes:
		lines = append(lines,
			"routes:",
			"  a            add a route; the local PORT field is focused first",
			"  o            open the selected route's public URL in a browser",
			"  J / K        move a route down / up (catch-all stays last)",
			"  p            pin the selected route to the top",
			"  w            apply staged changes",
			"  u            discard staged changes",
			"  /            filter routes by host, path or service",
			"  esc          back to the tunnel list",
		)
	case ViewForm:
		lines = append(lines,
			"add-route form:",
			"  tab          next field (port, hostname, service)",
			"  shift+tab    previous field",
			"  enter        stage the route",
			"  esc          cancel",
		)
	default:
		lines = append(lines,
			"tunnels:",
			"  n            create a tunnel (validated name)",
			"  d            delete the selected tunnel (type its name to confirm)",
			"  enter        open the tunnel's routes",
			"  t            show the connector token to run this tunnel elsewhere",
			"  r            refresh the list",
			"  esc          close this help",
		)
	}
	lines = append(lines,
		"",
		"anywhere:",
		"  ?            close this help",
		"  esc          close this help",
		"  q            close this help (then q again quits)",
		"  ctrl+c       quit immediately",
	)
	return strings.Join(lines, "\n")
}

// viewTunnels renders the tunnel list.
func (m Model) viewTunnels() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("cftui - tunnels") + "\n\n")
	if m.showHelp {
		b.WriteString(helpText(ViewTunnels) + "\n")
		return b.String()
	}
	if m.tokenTunnel != "" {
		b.WriteString(m.renderToken())
		return b.String()
	}
	if m.mode == "create-tunnel" {
		b.WriteString("new tunnel name: " + m.confirmName + "\n")
		return b.String()
	}
	if m.mode == "delete-tunnel" {
		name := ""
		if m.cursor < len(m.tunnels) {
			name = m.tunnels[m.cursor].Name
		}
		b.WriteString("type tunnel name to delete " + name + ": " + m.confirmName + "\n")
		return b.String()
	}
	if len(m.tunnels) == 0 {
		b.WriteString("no tunnels found\n")
		return b.String()
	}
	for i, t := range m.tunnels {
		line := fmt.Sprintf("%-8s %-30s %-10s connectors=%d routes=%d", m.badge(t.Name), t.Name, t.Status, t.ConnectorN, t.RouteN)
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// renderToken draws the connector-token panel. The token stays masked until the
// user presses v, because it is a live credential for running the tunnel.
func (m Model) renderToken() string {
	var b strings.Builder
	b.WriteString("connector token for " + m.tokenTunnel + "\n\n")
	b.WriteString("  run this tunnel elsewhere with: cloudflared tunnel run --token <token>\n\n")
	switch {
	case m.tokenLoading:
		b.WriteString("  fetching token...\n")
	case m.tokenErr != "":
		b.WriteString("  " + m.tokenErr + "\n")
	case m.tokenShown:
		b.WriteString("  " + m.token + "\n")
	default:
		b.WriteString("  " + maskToken(m.token) + "\n")
	}
	b.WriteString("\n")
	if m.tokenErr == "" && !m.tokenLoading {
		reveal := "v reveal"
		if m.tokenShown {
			reveal = "v hide"
		}
		b.WriteString("  " + reveal + "  t/esc close\n")
		b.WriteString("\n  This token grants connector access to the tunnel; keep it secret.\n")
		return b.String()
	}
	b.WriteString("  t/esc close\n")
	return b.String()
}

// maskToken hides a token's body but keeps its shape, so the user can confirm a
// token was fetched without it being readable over a shoulder or in a recording.
func maskToken(token string) string {
	if token == "" {
		return "(none)"
	}
	if len(token) <= 8 {
		return strings.Repeat("\u2022", len(token))
	}
	return token[:4] + strings.Repeat("\u2022", 12) + token[len(token)-4:]
}

// viewRoutes renders the route table and the detail pane for one route.
func (m Model) viewRoutes() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("routes - "+m.tunnel.Name) + "\n\n")
	if m.showHelp {
		b.WriteString(helpText(ViewRoutes) + "\n")
		return b.String()
	}
	if Changed(m.committed, m.routes) {
		b.WriteString(warnStyle.Render("[staged changes]") + "\n")
	}
	if m.disagree != "" {
		b.WriteString(warnStyle.Render("! "+m.disagree) + "\n")
	}
	if m.filtering {
		b.WriteString("filter: " + m.filter + "_\n")
	}
	if m.mode == "discard" {
		b.WriteString("discard staged changes? y/esc\n")
	}
	if len(m.routes) == 0 {
		b.WriteString("no routes\n")
		return b.String()
	}
	visible := FilterRoutes(m.routes, m.filter)
	if len(visible) == 0 {
		b.WriteString("no routes match " + m.filter + "\n")
		return b.String()
	}
	for _, r := range visible {
		host := r.Hostname
		if host == "" {
			host = "(catch-all)"
		}
		line := fmt.Sprintf("%-30s %-25s %s", host, r.Path, r.Service)
		if m.routeAt < len(m.routes) && r.Hostname == m.routes[m.routeAt].Hostname && r.Service == m.routes[m.routeAt].Service {
			b.WriteString(cursorStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if m.routeAt < len(m.routes) {
		d := m.routes[m.routeAt]
		b.WriteString("\n" + titleStyle.Render("detail") + "\n")
		b.WriteString(fmt.Sprintf("  hostname: %s\n  path:     %s\n  service:  %s\n",
			d.Hostname, d.Path, d.Service))
		if url := RouteURL(d); url != "" {
			b.WriteString("  url:      " + url + "   (press o to open)\n")
		}
	}
	return b.String()
}

// openURL returns a command that hands a URL to the platform opener, falling
// back through the common alternatives so it works on Linux and macOS.
func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		candidates := [][]string{
			{"xdg-open", url},
			{"open", url},
			{"wslview", url},
		}
		var lastErr error
		for _, c := range candidates {
			bin, err := exec.LookPath(c[0])
			if err != nil {
				lastErr = err
				continue
			}
			if err := exec.Command(bin, c[1:]...).Start(); err != nil {
				lastErr = err
				continue
			}
			return openURLMsg{url: url}
		}
		return openURLMsg{url: url, err: lastErr}
	}
}

type openURLMsg struct {
	url string
	err error
}

func validTunnelName(name string) bool {
	if len(name) < 1 || len(name) > 63 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// viewForm renders the add-route form. The port field is first and focused on
// open, and a live probe reports whether anything is listening before commit.
func (m Model) viewForm() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("add route - local port first") + "\n\n")
	if m.showHelp {
		b.WriteString(helpText(ViewForm) + "\n")
		return b.String()
	}

	b.WriteString(m.fieldRow("port", m.form.Port, "http://localhost:<port>"))
	if m.probed {
		if m.probe.OK {
			b.WriteString("      " + okStyle.Render(fmt.Sprintf("listening (%s)", m.probe.Latency.Round(time.Millisecond))) + "\n")
		} else {
			b.WriteString("      " + warnStyle.Render(fmt.Sprintf("nothing is listening on localhost:%s", m.form.Port)) + "\n")
		}
	}
	b.WriteString(m.fieldRow("hostname", m.form.Hostname, "app.example.com"))
	svc := m.form.Service
	if svc == "" {
		svc = "http"
	}
	b.WriteString(m.fieldRow("service", svc, "http") + "\n")
	return b.String()
}

// fieldRow renders one labelled form field, marking the focused one.
func (m Model) fieldRow(name, value, placeholder string) string {
	if value == "" {
		value = placeholder
	}
	label := fmt.Sprintf("  %-9s %s", name, value)
	if formFields[m.form.Focus] == name {
		return cursorStyle.Render("> "+label[2:]) + "\n"
	}
	return label + "\n"
}

// parsePort validates a port string and returns it as a number.
func parsePort(s string) (int, error) {
	if err := validate.PortString(s); err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return n, nil
}

// refreshDisagreement checks whether the local config and Cloudflare agree on
// this tunnel's hostnames, returning a command only when both sides are
// available. The local side is read synchronously because it is already in
// memory; only the remote side needs a round trip.
func (m Model) refreshDisagreement() tea.Cmd {
	m.disagree = ""
	if m.detect.LocalRules == nil || m.detect.RemoteHosts == nil || m.tunnel.Name == "" {
		return nil
	}
	localRules := m.detect.LocalRules(m.tunnel.Name)
	name := m.tunnel.Name
	remote := m.detect.RemoteHosts
	return func() tea.Msg {
		hosts, err := remote(context.Background(), name)
		return disagreeMsg{name: name, localRules: localRules, remoteHosts: hosts, err: err}
	}
}

type disagreeMsg struct {
	name        string
	localRules  []local.Rule
	remoteHosts []string
	err         error
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Styles for the interface.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	helpStyle   = lipgloss.NewStyle().Faint(true)
)
