package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectManagementPrefersConfigFile(t *testing.T) {
	in := DetectInput{
		TunnelID:          "aaaa1111-2222-3333-4444-555555555555",
		ConfigTunnelNames: []string{"aaaa1111-2222-3333-4444-555555555555"},
		CredentialIDs:     nil,
	}
	if got := DetectManagement(in); got != ManagedLocal {
		t.Fatalf("DetectManagement = %q, want %q", got, ManagedLocal)
	}
}

func TestDetectManagementFallsBackToCredentials(t *testing.T) {
	in := DetectInput{
		TunnelID:          "bbbb1111-2222-3333-4444-555555555555",
		ConfigTunnelNames: []string{"some-other-tunnel"},
		CredentialIDs:     []string{"bbbb1111-2222-3333-4444-555555555555"},
	}
	if got := DetectManagement(in); got != ManagedLocal {
		t.Fatalf("DetectManagement = %q, want %q", got, ManagedLocal)
	}
}

func TestDetectManagementDefaultsToRemote(t *testing.T) {
	cases := []struct {
		name string
		in   DetectInput
		want Management
	}{
		{
			name: "nothing matches is remote",
			in: DetectInput{
				TunnelID:          "cccc1111-2222-3333-4444-555555555555",
				ConfigTunnelNames: []string{"local-only"},
				CredentialIDs:     []string{"another-tunnel"},
			},
			want: ManagedRemote,
		},
		{
			name: "empty id is unknown, never remote",
			in:   DetectInput{},
			want: ManagedUnknown,
		},
		{
			name: "case-insensitive match still counts as local",
			in: DetectInput{
				TunnelID:          "DDDD1111-2222-3333-4444-555555555555",
				ConfigTunnelNames: []string{"dddd1111-2222-3333-4444-555555555555"},
			},
			want: ManagedLocal,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectManagement(tc.in); got != tc.want {
				t.Fatalf("DetectManagement = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiscoverCredentialsMissingDirIsNotAnError(t *testing.T) {
	home := t.TempDir()

	got, err := DiscoverCredentials(home)
	if err != nil {
		t.Fatalf("missing ~/.cloudflared must not be an error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no credentials, got %v", got)
	}

	dir := filepath.Join(home, ".cloudflared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tunnel-a.json", "tunnel-b.json", "cert.pem", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err = DiscoverCredentials(home)
	if err != nil {
		t.Fatalf("DiscoverCredentials: %v", err)
	}
	want := []string{"tunnel-a", "tunnel-b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("DiscoverCredentials = %v, want %v (only .json basenames)", got, want)
	}
}

func TestDisagreementReportsRuleCountMismatch(t *testing.T) {
	localRules := []Rule{
		{Hostname: "app.example.com", Service: "http://localhost:3000"},
		{Hostname: "api.example.com", Service: "http://localhost:8080"},
		{Service: "http_status:404"},
	}
	remoteHosts := []string{"app.example.com", "extra.example.com"}

	got := Disagreement("my-tunnel", localRules, remoteHosts)
	if got == "" {
		t.Fatal("differing views must produce a warning")
	}
	if !strings.Contains(got, "my-tunnel") {
		t.Errorf("warning should name the tunnel: %q", got)
	}
	if !strings.Contains(got, "api.example.com") {
		t.Errorf("warning should name the local-only host: %q", got)
	}
	if !strings.Contains(got, "extra.example.com") {
		t.Errorf("warning should name the remote-only host: %q", got)
	}
}

func TestDisagreementAgreesWhenEqual(t *testing.T) {
	cases := []struct {
		name        string
		localRules  []Rule
		remoteHosts []string
	}{
		{
			name:        "identical single host",
			localRules:  []Rule{{Hostname: "app.example.com", Service: "http://localhost:3000"}},
			remoteHosts: []string{"app.example.com"},
		},
		{
			name: "identical set in different order",
			localRules: []Rule{
				{Hostname: "b.example.com", Service: "http://localhost:3001"},
				{Hostname: "a.example.com", Service: "http://localhost:3002"},
			},
			remoteHosts: []string{"a.example.com", "b.example.com"},
		},
		{
			name:        "case-insensitive agreement",
			localRules:  []Rule{{Hostname: "App.Example.COM", Service: "http://localhost:3000"}},
			remoteHosts: []string{"app.example.com"},
		},
		{
			name:        "both empty agree",
			localRules:  nil,
			remoteHosts: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Disagreement("t", tc.localRules, tc.remoteHosts); got != "" {
				t.Fatalf("equivalent views must agree, got %q", got)
			}
		})
	}
}
