package local

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commentedConfig = `# cftui-managed
# do not lose me
tunnel: my-tunnel
credentials-file: /home/u/.cloudflared/my-tunnel.json
ingress:
  # public web
  - hostname: app.example.com
    service: http://localhost:3000 # the app
  - service: http_status:404
`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("seed %s: %v", p, err)
	}
	return p
}

func TestWritePreservesComments(t *testing.T) {
	p := writeTemp(t, "config.yml", commentedConfig)

	f, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rules, err := f.TunnelIngress("my-tunnel")
	if err != nil {
		t.Fatalf("TunnelIngress: %v", err)
	}
	rules = append(rules[:len(rules)-1], Rule{
		Hostname: "new.example.com", Service: "http://localhost:8080",
	}, rules[len(rules)-1])

	if err := f.SetTunnelIngress("my-tunnel", rules); err != nil {
		t.Fatalf("SetTunnelIngress: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	for _, want := range []string{"# cftui-managed", "do not lose me", "# public web", "the app"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("comment %q was lost; file now:\n%s", want, got)
		}
	}
	if !strings.Contains(string(got), "new.example.com") {
		t.Errorf("new rule missing from written file:\n%s", got)
	}

	bak, err := os.ReadFile(p + ".bak")
	if err != nil {
		t.Fatalf("expected backup %s.bak: %v", p, err)
	}
	if string(bak) != commentedConfig {
		t.Errorf("backup does not match the pre-write content:\n%s", bak)
	}
}

func TestSetTunnelIngressRoundTrip(t *testing.T) {
	p := writeTemp(t, "config.yml", commentedConfig)

	f, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rules, err := f.TunnelIngress("my-tunnel")
	if err != nil {
		t.Fatalf("TunnelIngress: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("want 2 seeded rules, got %d: %+v", len(rules), rules)
	}

	rules = append(rules[:len(rules)-1], Rule{
		Hostname: "api.example.com", Service: "https://localhost:8443",
	}, rules[len(rules)-1])
	if err := f.SetTunnelIngress("my-tunnel", rules); err != nil {
		t.Fatalf("SetTunnelIngress: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	re, err := Load(p)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, err := re.TunnelIngress("my-tunnel")
	if err != nil {
		t.Fatalf("reload TunnelIngress: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rules after add, got %d: %+v", len(got), got)
	}
	if got[1].Hostname != "api.example.com" {
		t.Errorf("new rule not in position 1: %+v", got)
	}
	last := got[len(got)-1]
	if last.Hostname != "" || last.Service != "http_status:404" {
		t.Errorf("catch-all must be last and hostname-less, got %+v", last)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "absent", "config.yml")
	f, err := Load(p)
	if err != nil {
		t.Fatalf("Load on missing file must not error, got: %v", err)
	}
	if err := f.SetTunnelIngress("t", []Rule{{Hostname: "a.example.com", Service: "http://localhost:1"}}); err != nil {
		t.Fatalf("SetTunnelIngress: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save on missing file must create it: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected file created at %s: %v", p, err)
	}
}

func TestSaveOmitsEmptyFields(t *testing.T) {
	p := writeTemp(t, "config.yml", "tunnel: t\ningress:\n  - service: http_status:404\n")
	f, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := f.SetTunnelIngress("t", []Rule{
		{Hostname: "x.example.com", Service: "http://localhost:1"},
		{Service: "http_status:404"},
	}); err != nil {
		t.Fatalf("SetTunnelIngress: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := os.ReadFile(p)
	if strings.Contains(string(got), `hostname: ""`) {
		t.Errorf("empty hostname must be omitted, file:\n%s", got)
	}
}
