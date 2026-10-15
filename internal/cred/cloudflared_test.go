package cred

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// shellScriptsNoop skips a test that stands in a POSIX shell script for the
// cloudflared binary. Windows has no /bin/sh and execs only .exe files, so
// these stubs cannot run there; the behaviour they cover is exercised on
// Linux and macOS.
func shellScriptsNoop(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub is a POSIX shell script; Windows cannot exec it")
	}
}

func TestDetectCloudflaredMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CLOUDFLARED", "")
	status := DetectCloudflared(context.Background())
	if status.Found || status.Path != "" {
		t.Fatalf("got %+v, want missing binary", status)
	}
}

func TestDetectCloudflaredUsesConfiguredPath(t *testing.T) {
	shellScriptsNoop(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'cloudflared version 2022.1.0\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOUDFLARED", path)
	status := DetectCloudflared(context.Background())
	if !status.Found || status.Path != path || !status.SupportsBanner {
		t.Fatalf("unexpected status: %+v", status)
	}
}

// stubCloudflared writes a shell script standing in for the real binary and
// points CLOUDFLARED at it, so TunnelToken can be exercised without network or
// a real tunnel.
func stubCloudflared(t *testing.T, body string) {
	t.Helper()
	shellScriptsNoop(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("CLOUDFLARED", path)
}

// TestTunnelTokenReturnsTrimmedToken checks the happy path trims cloudflared's
// trailing newline so the stored token is usable verbatim.
func TestTunnelTokenReturnsTrimmedToken(t *testing.T) {
	stubCloudflared(t, "printf '%s\\n' eyJhIjoiTOKEN.payload\n")
	tok, err := TunnelToken(context.Background(), "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "eyJhIjoiTOKEN.payload" {
		t.Fatalf("token = %q, want the trimmed value", tok)
	}
}

// TestTunnelTokenEmptyNameRefuses stops a blank name reaching the CLI.
func TestTunnelTokenEmptyNameRefuses(t *testing.T) {
	stubCloudflared(t, "echo should-not-run\n")
	if _, err := TunnelToken(context.Background(), "   "); err == nil {
		t.Fatal("empty tunnel name must be refused")
	}
}

// TestTunnelTokenReportsFailure checks a non-zero exit (e.g. deleted tunnel) is
// surfaced as a TunnelTokenError carrying the tunnel name.
func TestTunnelTokenReportsFailure(t *testing.T) {
	stubCloudflared(t, "echo 'tunnel not found' >&2\nexit 1\n")
	_, err := TunnelToken(context.Background(), "gone")
	if err == nil {
		t.Fatal("a failed fetch must return an error")
	}
	var tte *TunnelTokenError
	if !errors.As(err, &tte) {
		t.Fatalf("error type = %T, want *TunnelTokenError", err)
	}
	if tte.Tunnel != "gone" {
		t.Fatalf("error names tunnel %q, want gone", tte.Tunnel)
	}
	if !strings.Contains(err.Error(), "tunnel not found") {
		t.Fatalf("error must carry cloudflared's message, got %q", err.Error())
	}
}

// TestTunnelTokenRejectsEmptyOutput covers a CLI that exits 0 with no token.
func TestTunnelTokenRejectsEmptyOutput(t *testing.T) {
	stubCloudflared(t, "exit 0\n")
	if _, err := TunnelToken(context.Background(), "app"); err == nil {
		t.Fatal("empty output must be an error, not an empty token")
	}
}

// TestTunnelTokenErrorDoesNotLeakToken guards the error path from echoing a
// credential when the CLI writes the token to stdout but exits non-zero.
func TestTunnelTokenErrorDoesNotLeakToken(t *testing.T) {
	stubCloudflared(t, "echo 'SECRET-VALUE'\necho 'boom' >&2\nexit 1\n")
	_, err := TunnelToken(context.Background(), "app")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "SECRET-VALUE") {
		t.Fatalf("error leaked the token: %q", err.Error())
	}
}
