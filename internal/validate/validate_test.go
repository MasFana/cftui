package validate

import (
	"strings"
	"testing"
)

func TestHostname(t *testing.T) {
	cases := []struct {
		name, host, zone string
		wantErr          bool
	}{
		{"valid in zone", "grafana.example.com", "example.com", false},
		{"valid apex", "example.com", "example.com", false},
		{"valid no zone constraint", "a.b.c", "", false},
		{"outside zone", "evil.other.com", "example.com", true},
		{"single label", "grafana", "", true},
		{"empty", "", "example.com", true},
		{"underscore", "bad_host.example.com", "example.com", true},
		{"leading dash", "-bad.example.com", "example.com", true},
		{"trailing dash", "bad-.example.com", "example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Hostname(tc.host, tc.zone)
			if tc.wantErr && err == nil {
				t.Fatalf("want error for %q in %q", tc.host, tc.zone)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestHostnameZoneIsCaseInsensitive(t *testing.T) {
	if err := Hostname("App.Example.COM", "example.com"); err != nil {
		t.Fatalf("zone match must be case-insensitive: %v", err)
	}
}

func TestPortString(t *testing.T) {
	for _, ok := range []string{"1", "80", "3000", "65535"} {
		if err := PortString(ok); err != nil {
			t.Fatalf("port %s should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "65536", "-1", "abc", "", "80.5"} {
		if err := PortString(bad); err == nil {
			t.Fatalf("port %q should be rejected", bad)
		}
	}
}

func TestService(t *testing.T) {
	valid := []string{
		"http://localhost:3000",
		"https://127.0.0.1:8443",
		"ssh://localhost:22",
		"tcp://localhost:5432",
		"unix:/var/run/app.sock",
		"http_status:404",
		"http_status:503",
		"hello_world",
	}
	for _, s := range valid {
		if err := Service(s); err != nil {
			t.Fatalf("service %q should be valid: %v", s, err)
		}
	}
	invalid := []string{
		"", "localhost:3000", "http://localhost", "ftp://localhost:21",
		"unix:relative.sock", "http_status:99", "http_status", "http://:3000",
	}
	for _, s := range invalid {
		if err := Service(s); err == nil {
			t.Fatalf("service %q should be rejected", s)
		}
	}
	if err := Service("http://localhost:70000"); err == nil {
		t.Fatal("out-of-range port in service should be rejected")
	}
}

func TestDuplicate(t *testing.T) {
	existing := []struct{ Hostname, Path string }{
		{"app.example.com", ""},
		{"app.example.com", "/api/*"},
	}
	if err := Duplicate(existing, "app.example.com", ""); err == nil {
		t.Fatal("exact duplicate must be rejected")
	}
	if err := Duplicate(existing, "APP.example.com", ""); err == nil {
		t.Fatal("duplicate check must be case-insensitive")
	}
	if err := Duplicate(existing, "app.example.com", "/other/*"); err != nil {
		t.Fatalf("different path is not a duplicate: %v", err)
	}
	if err := Duplicate(existing, "new.example.com", ""); err != nil {
		t.Fatalf("new hostname is not a duplicate: %v", err)
	}
}

func TestCatchAllMutable(t *testing.T) {
	err := CatchAllMutable(0)
	if err == nil || !strings.Contains(err.Error(), "catch-all") {
		t.Fatal("removing the catch-all must be refused with a catch-all message")
	}
	if err := CatchAllMutable(1); err != nil {
		t.Fatalf("a present catch-all is fine: %v", err)
	}
}
