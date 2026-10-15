package cf

import (
	"fmt"
	"strings"
	"testing"
)

var _ API = (*CloudflareAPI)(nil)

func TestNewCloudflareAPIRejectsEmptyToken(t *testing.T) {
	if _, err := NewCloudflareAPI(""); err == nil {
		t.Fatal("empty token must be rejected")
	}
}

func TestNewCloudflareAPIAcceptsToken(t *testing.T) {
	c, err := NewCloudflareAPI("some-token-value")
	if err != nil {
		t.Fatalf("NewCloudflareAPI: %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestTokenNeverPrinted(t *testing.T) {
	const token = "SUPERSECRETTOKEN123"
	c, err := NewCloudflareAPI(token)
	if err != nil {
		t.Fatalf("NewCloudflareAPI: %v", err)
	}
	if got := strings.Join([]string{
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%+v", c),
		fmt.Sprintf("%#v", c),
	}, " "); strings.Contains(got, token) {
		t.Fatalf("token leaked through a printable representation: %s", got)
	}

	_, err = c.ListTunnels(nil, "")
	if err == nil {
		t.Fatal("expected an error for an empty account id")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked into an error: %v", err)
	}
}

func TestIngressRoundTripKeepsOrder(t *testing.T) {
	routes := []Route{
		{Hostname: "a.example.com", Service: "http://localhost:3000"},
		{Hostname: "b.example.com", Path: "/api", Service: "http://localhost:8080", HTTPHost: "a.example.com"},
		{Service: "http_status:404"},
	}
	back := routesFromIngress(ingressFromRoutes(routes))
	if len(back) != len(routes) {
		t.Fatalf("want %d routes, got %d: %+v", len(routes), len(back), back)
	}
	for i := range routes {
		if back[i].Hostname != routes[i].Hostname ||
			back[i].Path != routes[i].Path ||
			back[i].Service != routes[i].Service ||
			back[i].HTTPHost != routes[i].HTTPHost {
			t.Errorf("route %d differs:\n got %+v\nwant %+v", i, back[i], routes[i])
		}
	}
	if last := back[len(back)-1]; last.Hostname != "" || last.Service != "http_status:404" {
		t.Errorf("catch-all must stay last: %+v", last)
	}
}
