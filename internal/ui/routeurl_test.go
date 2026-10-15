package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/MasFana/cftui/internal/cf"
)

func TestRouteURLBuildsPublicHostname(t *testing.T) {
	cases := []struct {
		name string
		rule cf.Route
		want string
	}{
		{
			name: "plain hostname",
			rule: cf.Route{Hostname: "app.example.com", Service: "http://localhost:3000"},
			want: "https://app.example.com",
		},
		{
			name: "hostname with path",
			rule: cf.Route{Hostname: "app.example.com", Path: "/api", Service: "http://localhost:8080"},
			want: "https://app.example.com/api",
		},
		{
			name: "path without leading slash is normalised",
			rule: cf.Route{Hostname: "app.example.com", Path: "api", Service: "http://localhost:8080"},
			want: "https://app.example.com/api",
		},
		{
			name: "catch-all has no url",
			rule: cf.Route{Service: "http_status:404"},
			want: "",
		},
		{
			name: "404 service has no url",
			rule: cf.Route{Hostname: "x.example.com", Service: "http_status:404"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RouteURL(tc.rule); got != tc.want {
				t.Fatalf("RouteURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFilterRoutesMatchesAndKeepsCatchAll(t *testing.T) {
	routes := []cf.Route{
		{Hostname: "app.example.com", Service: "http://localhost:3000"},
		{Hostname: "api.example.com", Service: "http://localhost:8080"},
		{Service: "http_status:404"},
	}

	got := FilterRoutes(routes, "api")
	if len(got) != 2 {
		t.Fatalf("want the api route plus the catch-all, got %d: %+v", len(got), got)
	}
	if got[0].Hostname != "api.example.com" {
		t.Errorf("filter kept the wrong route: %+v", got[0])
	}
	if !cf.IsCatchAll(got[1]) {
		t.Errorf("catch-all must survive filtering: %+v", got[1])
	}

	if out := FilterRoutes(routes, "   "); len(out) != len(routes) {
		t.Errorf("blank query must return all routes, got %d", len(out))
	}

	if out := FilterRoutes(routes, "nomatch"); len(out) != 1 || !cf.IsCatchAll(out[0]) {
		t.Errorf("a query with no hits should leave only the catch-all, got %+v", out)
	}
}

func TestSlashKeyStartsFilteringAndNarrowsList(t *testing.T) {
	m := modelWithRoutes(t,
		cf.Route{Hostname: "app.example.com", Service: "http://localhost:3000"},
		cf.Route{Hostname: "api.example.com", Service: "http://localhost:8080"},
		cf.Route{Service: "http_status:404"},
	)

	next, _ := m.Update(keyRune('/'))
	m = next.(Model)
	if !m.filtering {
		t.Fatal("/ must start filtering")
	}

	for _, r := range "api" {
		next, _ = m.Update(keyRune(r))
		m = next.(Model)
	}
	if m.filter != "api" {
		t.Fatalf("filter = %q, want %q", m.filter, "api")
	}
	if got := FilterRoutes(m.routes, m.filter); len(got) != 2 {
		t.Fatalf("filtered routes = %d, want 2 (api + catch-all)", len(got))
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.filtering || m.filter != "" {
		t.Fatalf("esc must clear the filter, got filtering=%v filter=%q", m.filtering, m.filter)
	}
}

func TestHelpKeyTogglesReference(t *testing.T) {
	m := modelWithRoutes(t, cf.Route{Hostname: "a.example.com", Service: "http://localhost:1"})

	next, _ := m.Update(keyRune('?'))
	m = next.(Model)
	if !m.showHelp {
		t.Fatal("? must show help")
	}
	if view := m.View(); !strings.Contains(view, "cftui key reference") {
		t.Fatalf("help view must contain the reference, got:\n%s", view)
	}

	next, _ = m.Update(keyRune('?'))
	m = next.(Model)
	if m.showHelp {
		t.Fatal("? must hide help again")
	}
}

func TestOpenKeyBuildsURLForSelectedRoute(t *testing.T) {
	m := modelWithRoutes(t,
		cf.Route{Hostname: "app.example.com", Service: "http://localhost:3000"},
		cf.Route{Service: "http_status:404"},
	)

	next, cmd := m.Update(keyRune('o'))
	m = next.(Model)
	if m.lastOpenURL != "https://app.example.com" {
		t.Fatalf("lastOpenURL = %q, want https://app.example.com", m.lastOpenURL)
	}
	if cmd == nil {
		t.Fatal("o must produce a command that opens the URL")
	}
}

func TestOpenKeyOnCatchAllReportsNoURL(t *testing.T) {
	m := modelWithRoutes(t, cf.Route{Service: "http_status:404"})

	next, _ := m.Update(keyRune('o'))
	m = next.(Model)
	if m.lastOpenURL != "" {
		t.Fatalf("catch-all must not open a URL, got %q", m.lastOpenURL)
	}
	if !strings.Contains(m.errText, "no public URL") {
		t.Fatalf("want a no-URL message, got %q", m.errText)
	}
}

func modelWithRoutes(t *testing.T, routes ...cf.Route) Model {
	t.Helper()
	m := Model{view: ViewRoutes, tunnel: cf.Tunnel{Name: "t"}}
	m.routes = cf.EnsureCatchAllLast(routes)
	m.committed = append([]cf.Route(nil), m.routes...)
	return m
}

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
