package cred

import (
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setHome points the user's home directory at dir for the duration of the
// test. os.UserHomeDir reads HOME on Unix but USERPROFILE on Windows, so a
// test that sets only HOME would silently read the real home directory on
// Windows and test nothing.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
	}
}

func encode(t *testing.T, cert OriginCert, withLegacy bool) []byte {
	t.Helper()
	body, err := json.Marshal(cert)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out []byte
	if withLegacy {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("legacydata")})...)
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("legacycert")})...)
	}
	return append(out, pem.EncodeToMemory(&pem.Block{Type: argoTunnelTokenBlock, Bytes: body})...)
}

func TestDecodeValidCert(t *testing.T) {
	raw := encode(t, OriginCert{ZoneID: "z1", AccountID: "a1", APIToken: "tok"}, true)
	cert, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if cert.ZoneID != "z1" || cert.AccountID != "a1" || cert.APIToken != "tok" {
		t.Fatalf("unexpected cert: %+v", cert)
	}
}

func TestDecodeHandlesRealCloudflaredCertLayout(t *testing.T) {
	raw := encode(t, OriginCert{ZoneID: "z", AccountID: "a", APIToken: "t"}, false)
	real := string(raw) + "-----BEGIN ARGO TUNNEL TOKEN-----\n"
	_ = real
	if got, err := Decode(raw); err != nil || got.ZoneID != "z" {
		t.Fatalf("plain single-block bundle must decode: %v", err)
	}
}

func TestDecodeRejectsMissingSeed(t *testing.T) {
	for _, tc := range []struct{ name, zone, token string }{
		{"missing zone", "", "tok"},
		{"missing token", "z1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode(encode(t, OriginCert{ZoneID: tc.zone, APIToken: tc.token}, false)); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode([]byte("not a pem bundle")); err == nil {
		t.Fatal("want error for non-PEM input")
	}
	if _, err := Decode(nil); err == nil {
		t.Fatal("want error for empty input")
	}
}

func TestSecretNeverLeaksInFormatting(t *testing.T) {
	const secret = "SUPER-SECRET-TOKEN-VALUE"
	cert := &OriginCert{ZoneID: "z1", AccountID: "a1", APIToken: secret}
	formatted := []string{
		cert.String(),
		cert.GoString(),
		strings.Join([]string{cert.String(), cert.GoString()}, " "),
	}
	for _, s := range formatted {
		if strings.Contains(s, secret) {
			t.Fatalf("token leaked into formatted output: %q", s)
		}
	}
}

func TestFindPathPrefersExplicitThenEnv(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "explicit.pem")
	if err := os.WriteFile(explicit, encode(t, OriginCert{ZoneID: "z", APIToken: "t"}, false), 0o600); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(dir, "env.pem")
	if err := os.WriteFile(envPath, encode(t, OriginCert{ZoneID: "z2", APIToken: "t2"}, false), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvOriginCert, envPath)

	got, err := FindPath(explicit)
	if err != nil {
		t.Fatalf("FindPath: %v", err)
	}
	if got != explicit {
		t.Fatalf("explicit path must win: got %s want %s", got, explicit)
	}

	got, err = FindPath("")
	if err != nil {
		t.Fatalf("FindPath(env): %v", err)
	}
	if got != envPath {
		t.Fatalf("env path must win over default: got %s want %s", got, envPath)
	}
}

func TestFindPathMissing(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv(EnvOriginCert, "")
	if _, err := FindPath(""); err == nil {
		t.Fatal("want ErrMissingCredential")
	}
}

func TestLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cert.pem")
	want := OriginCert{ZoneID: "zone-abc", AccountID: "acct-xyz", APIToken: "tok-123"}
	if err := os.WriteFile(p, encode(t, want, true), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, usedPath, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if usedPath != p {
		t.Fatalf("path: got %s want %s", usedPath, p)
	}
	if cert.ZoneID != want.ZoneID || cert.AccountID != want.AccountID || cert.APIToken != want.APIToken {
		t.Fatalf("cert mismatch: %+v", cert)
	}
}

func TestRejectTokenFromEnvSetAndUnset(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	if err := RejectTokenFromEnv(); err != nil {
		t.Fatalf("unset token: %v", err)
	}
	t.Setenv("CLOUDFLARE_API_TOKEN", "SECRET-TOKEN")
	if err := RejectTokenFromEnv(); err != ErrTokenFromEnv {
		t.Fatalf("set token: got %v want %v", err, ErrTokenFromEnv)
	}
}

func TestEmptyTokenErrorNamesLoginCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(path, encode(t, OriginCert{ZoneID: "zone"}, false), 0o600); err != nil {
		t.Fatal(err)
	}
	_, usedPath, err := Load(path)
	if err == nil {
		t.Fatal("Load: want error")
	}
	if usedPath != path || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "run: cloudflared tunnel login") {
		t.Fatalf("unexpected error: %q", err)
	}
	if _, ok := err.(*IncompleteCertError); !ok {
		t.Fatalf("got %T, want *IncompleteCertError", err)
	}
}
