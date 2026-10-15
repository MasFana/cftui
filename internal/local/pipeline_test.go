package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeVerifier struct {
	calls   [][]string
	outputs map[string]string
	failOn  map[string]bool
}

func (f *fakeVerifier) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := append([]string{name}, args...)
	f.calls = append(f.calls, cmd)
	key := strings.Join(cmd, " ")
	if f.failOn[key] {
		return []byte(f.outputs[key]), errors.New("exit status 1")
	}
	if out, ok := f.outputs[key]; ok {
		return []byte(out), nil
	}
	return nil, nil
}

func (f *fakeVerifier) ran(substr string) bool {
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), substr) {
			return true
		}
	}
	return false
}

func present(bins ...string) func(string) (string, error) {
	return func(bin string) (string, error) {
		for _, b := range bins {
			if bin == b {
				return "/usr/bin/" + bin, nil
			}
		}
		return "", errors.New("not found")
	}
}

func pipelineFixture(t *testing.T, v Verifier, look func(string) (string, error)) (*Pipeline, *File) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("# keep me\ntunnel: t1\ningress:\n  - service: http_status:404\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetTunnelIngress("t1", []Rule{
		{Hostname: "app.example.com", Service: "http://localhost:3000"},
		{Service: "http_status:404"},
	}); err != nil {
		t.Fatal(err)
	}
	return &Pipeline{Verifier: v, LookPath: look}, f
}

func TestPipelineValidatesAndReloadsSystemdUser(t *testing.T) {
	v := &fakeVerifier{outputs: map[string]string{
		"systemctl --user is-active cloudflared": "active\n",
	}}
	p, f := pipelineFixture(t, v, present("systemctl", "cloudflared"))

	res, err := p.Apply(context.Background(), f)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Validated {
		t.Error("expected Validated=true")
	}
	if !res.Reloaded {
		t.Error("expected Reloaded=true")
	}
	if res.Strategy != ReloadUserSystemd {
		t.Errorf("strategy = %q, want %q", res.Strategy, ReloadUserSystemd)
	}
	if !v.ran("cloudflared tunnel ingress validate") {
		t.Error("validate was not executed")
	}
	if !v.ran("systemctl --user restart cloudflared") {
		t.Error("user-level restart was not executed")
	}
	if _, err := os.Stat(f.Path() + backupSuffix); err != nil {
		t.Errorf(".bak was not written: %v", err)
	}
}

func TestPipelineSurfacesValidateOutputVerbatim(t *testing.T) {
	const cliErr = "cloudflared: invalid ingress rule: service is required"
	v := &fakeVerifier{
		outputs: map[string]string{},
		failOn:  map[string]bool{},
	}
	p, f := pipelineFixture(t, v, present("cloudflared"))
	v.failOn["cloudflared tunnel ingress validate --config "+f.Path()] = true
	v.outputs["cloudflared tunnel ingress validate --config "+f.Path()] = cliErr

	res, err := p.Apply(context.Background(), f)
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("err = %v, want ErrValidationFailed", err)
	}
	if !strings.Contains(err.Error(), cliErr) {
		t.Errorf("error must carry cloudflared output verbatim, got: %v", err)
	}
	if res.Validated {
		t.Error("Validated must be false when validation fails")
	}
}

func TestPipelineWithoutCloudflaredSkipsValidation(t *testing.T) {
	v := &fakeVerifier{}
	p, f := pipelineFixture(t, v, func(string) (string, error) { return "", errors.New("not found") })

	res, err := p.Apply(context.Background(), f)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Validated || res.Reloaded {
		t.Error("nothing should run without cloudflared installed")
	}
	if res.ReloadNote == "" {
		t.Error("expected a note explaining cloudflared was missing")
	}
	if _, err := os.Stat(f.Path()); err != nil {
		t.Errorf("the write must still have happened: %v", err)
	}
}

func TestPipelinePreservesValidateOutputExactly(t *testing.T) {
	const raw = "warning: no TLS\nconfig is valid\n\n"
	v := &fakeVerifier{
		outputs: map[string]string{"cloudflared tunnel ingress validate --config placeholder": raw},
		failOn:  map[string]bool{},
	}
	p, f := pipelineFixture(t, v, present("cloudflared"))
	key := "cloudflared tunnel ingress validate --config " + f.Path()
	v.outputs[key] = raw

	res, err := p.Apply(context.Background(), f)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.ValidateOut != raw {
		t.Errorf("ValidateOut = %q, want it byte-for-byte %q", res.ValidateOut, raw)
	}
}

func TestDetectReloadPrefersUserSystemd(t *testing.T) {
	v := &fakeVerifier{outputs: map[string]string{
		"systemctl --user is-active cloudflared": "active\n",
		"systemctl is-active cloudflared":        "active\n",
	}}
	p := &Pipeline{Verifier: v, LookPath: present("systemctl")}
	got, ok := p.DetectReload()
	if !ok || got != ReloadUserSystemd {
		t.Fatalf("DetectReload = (%q, %v), want (%q, true)", got, ok, ReloadUserSystemd)
	}
}

func TestDetectReloadFallsBackToSystem(t *testing.T) {
	v := &fakeVerifier{outputs: map[string]string{
		"systemctl --user is-active cloudflared": "inactive\n",
		"systemctl is-active cloudflared":        "active\n",
	}}
	p := &Pipeline{Verifier: v, LookPath: present("systemctl")}
	got, ok := p.DetectReload()
	if !ok || got != ReloadSystemd {
		t.Fatalf("DetectReload = (%q, %v), want (%q, true)", got, ok, ReloadSystemd)
	}
}

func TestDetectReloadNoneWhenNoService(t *testing.T) {
	p := &Pipeline{Verifier: &fakeVerifier{}, LookPath: func(string) (string, error) {
		return "", errors.New("not found")
	}}
	if got, ok := p.DetectReload(); ok || got != ReloadNone {
		t.Fatalf("DetectReload = (%q, %v), want (%q, false)", got, ok, ReloadNone)
	}
}
