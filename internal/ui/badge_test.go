package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MasFana/cftui/internal/cf"
	"github.com/MasFana/cftui/internal/local"
)

func TestBadgeNamesOwningSide(t *testing.T) {
	cases := []struct {
		management local.Management
		want       string
	}{
		{local.ManagedLocal, "MANUAL"},
		{local.ManagedRemote, "MANAGED"},
		{local.ManagedUnknown, "UNKNOWN"},
		{"", "UNKNOWN"},
	}
	for _, tc := range cases {
		m := NewWithDetect(Backends{}, Detect{
			Management: map[string]local.Management{"t": tc.management},
		})
		if got := m.badge("t"); got != tc.want {
			t.Errorf("badge(%q) = %q, want %q", tc.management, got, tc.want)
		}
	}
}

func TestTunnelListRendersBadges(t *testing.T) {
	m := NewWithDetect(Backends{}, Detect{
		Management: map[string]local.Management{
			"local-one":  local.ManagedLocal,
			"remote-one": local.ManagedRemote,
		},
	})
	m.tunnels = []cf.Tunnel{
		{Name: "local-one", Status: "healthy"},
		{Name: "remote-one", Status: "healthy"},
	}

	view := m.viewTunnels()
	if !strings.Contains(view, "MANUAL") {
		t.Errorf("local tunnel must be badged MANUAL, got:\n%s", view)
	}
	if !strings.Contains(view, "MANAGED") {
		t.Errorf("remote tunnel must be badged MANAGED, got:\n%s", view)
	}
}

func modelWithDetect(routes []cf.Route, d Detect) Model {
	m := Model{view: ViewRoutes, tunnel: cf.Tunnel{Name: "t"}, detect: d}
	m.routes = cf.EnsureCatchAllLast(routes)
	m.committed = append([]cf.Route(nil), m.routes...)
	return m
}

func TestDisagreementFooterShowsWhenSidesDiffer(t *testing.T) {
	d := Detect{
		LocalRules: func(string) []local.Rule {
			return []local.Rule{{Hostname: "app.example.com", Service: "http://localhost:3000"}}
		},
		RemoteHosts: func(context.Context, string) ([]string, error) {
			return []string{"app.example.com", "extra.example.com"}, nil
		},
	}
	m := modelWithDetect([]cf.Route{{Hostname: "app.example.com", Service: "http://localhost:3000"}}, d)

	next, cmd := m.Update(routeListMsg{
		tunnel: cf.Tunnel{Name: "t"},
		routes: []cf.Route{{Hostname: "app.example.com", Service: "http://localhost:3000"}},
	})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("loading routes with detection wired must trigger the disagreement check")
	}

	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.disagree == "" {
		t.Fatal("differing sides must produce a warning")
	}
	if !strings.Contains(m.disagree, "extra.example.com") {
		t.Errorf("warning must name the remote-only host, got %q", m.disagree)
	}
	if view := m.View(); !strings.Contains(view, "differs between the local config and Cloudflare") {
		t.Errorf("routes view must show the disagreement warning, got:\n%s", view)
	}
}

func TestDisagreementFooterSilentWhenSidesAgree(t *testing.T) {
	same := []local.Rule{{Hostname: "app.example.com", Service: "http://localhost:3000"}}
	d := Detect{
		LocalRules: func(string) []local.Rule { return same },
		RemoteHosts: func(context.Context, string) ([]string, error) {
			return []string{"app.example.com"}, nil
		},
	}
	m := modelWithDetect([]cf.Route{{Hostname: "app.example.com", Service: "http://localhost:3000"}}, d)

	next, cmd := m.Update(routeListMsg{
		tunnel: cf.Tunnel{Name: "t"},
		routes: []cf.Route{{Hostname: "app.example.com", Service: "http://localhost:3000"}},
	})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected a disagreement check command")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.disagree != "" {
		t.Fatalf("agreeing sides must stay silent, got %q", m.disagree)
	}
	if view := m.View(); strings.Contains(view, "differs between") {
		t.Errorf("no warning should render when sides agree, got:\n%s", view)
	}
}

func TestDisagreementCheckSkippedWithoutDetection(t *testing.T) {
	m := modelWithDetect([]cf.Route{{Hostname: "a.example.com", Service: "http://localhost:1"}}, Detect{})

	next, cmd := m.Update(routeListMsg{
		tunnel: cf.Tunnel{Name: "t"},
		routes: []cf.Route{{Hostname: "a.example.com", Service: "http://localhost:1"}},
	})
	m = next.(Model)
	if cmd != nil {
		t.Error("with no detection wired there is nothing to check")
	}
	if m.disagree != "" {
		t.Errorf("disagree must stay empty, got %q", m.disagree)
	}
}

func TestDisagreementRemoteErrorIsNotShown(t *testing.T) {
	d := Detect{
		LocalRules: func(string) []local.Rule { return nil },
		RemoteHosts: func(context.Context, string) ([]string, error) {
			return nil, errors.New("api unavailable")
		},
	}
	m := modelWithDetect(nil, d)

	next, cmd := m.Update(routeListMsg{tunnel: cf.Tunnel{Name: "t"}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected a check command")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.disagree != "" {
		t.Fatalf("an unreachable API must not fabricate a disagreement, got %q", m.disagree)
	}
}

func TestManagementDefaultsToUnknown(t *testing.T) {
	m := NewWithDetect(Backends{}, Detect{})
	if got := m.Management("missing"); got != local.ManagedUnknown {
		t.Fatalf("Management = %q, want %q", got, local.ManagedUnknown)
	}
}
