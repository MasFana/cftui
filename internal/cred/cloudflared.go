package cred

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CloudflaredStatus describes the local cloudflared binary as discovered on PATH.
type CloudflaredStatus struct {
	Found          bool
	Path           string
	Version        string
	SupportsBanner bool
}

var versionPattern = regexp.MustCompile(`(?i)(?:^|\s)v?(\d+)\.(\d+)\.(\d+)(?:\s|$)`)

// DetectCloudflared looks for the cloudflared binary and reports its version.
func DetectCloudflared(ctx context.Context) CloudflaredStatus {
	path := os.Getenv("CLOUDFLARED")
	if path == "" {
		var err error
		path, err = exec.LookPath("cloudflared")
		if err != nil {
			return CloudflaredStatus{}
		}
	}

	status := CloudflaredStatus{Found: true, Path: path}
	versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(versionCtx, path, "--version").Output()
	if err != nil {
		return status
	}
	status.Version = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	status.SupportsBanner = supportsBanner(status.Version)
	return status
}

func supportsBanner(version string) bool {
	match := versionPattern.FindStringSubmatch(version)
	if match == nil {
		return false
	}
	parts := [3]int{}
	for i := range parts {
		parts[i], _ = strconv.Atoi(match[i+1])
	}
	return parts[0] > 2022 || parts[0] == 2022 && (parts[1] > 1 || parts[1] == 1 && parts[2] >= 0)
}

// TunnelTokenError explains why a connector token could not be fetched. The
// underlying cloudflared failure is wrapped so callers can show their own hint.
type TunnelTokenError struct {
	Tunnel string
	Err    error
}

func (e *TunnelTokenError) Error() string {
	return fmt.Sprintf("cloudflared tunnel token %s: %v", e.Tunnel, e.Err)
}

func (e *TunnelTokenError) Unwrap() error { return e.Err }

// TunnelToken fetches the connector token that lets another host run the named
// tunnel. The token is a live credential: it grants connector access to that
// tunnel and must be handled like a secret. cloudflared prints nothing
// separately, so the whole trimmed stdout is the token. It fails when the
// tunnel does not exist or was deleted.
func TunnelToken(ctx context.Context, tunnel string) (string, error) {
	if strings.TrimSpace(tunnel) == "" {
		return "", &TunnelTokenError{Tunnel: tunnel, Err: fmt.Errorf("empty tunnel name")}
	}
	status := DetectCloudflared(ctx)
	if !status.Found {
		return "", &TunnelTokenError{Tunnel: tunnel, Err: fmt.Errorf("cloudflared not found on PATH")}
	}
	tokenCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(tokenCtx, status.Path, "tunnel", "token", tunnel).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", &TunnelTokenError{Tunnel: tunnel, Err: fmt.Errorf("%s", firstLine(ee.Stderr))}
		}
		return "", &TunnelTokenError{Tunnel: tunnel, Err: err}
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", &TunnelTokenError{Tunnel: tunnel, Err: fmt.Errorf("cloudflared returned an empty token")}
	}
	return token, nil
}

// firstLine reduces cloudflared's multi-line diagnostics to one readable line.
func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "cloudflared reported no details"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// StartupScopeBlocker reports whether OAuth login is unavailable locally.
func StartupScopeBlocker(ctx context.Context) (string, bool) {
	status := DetectCloudflared(ctx)
	if !status.Found {
		return "cloudflared not found on PATH: install it, then run: cloudflared tunnel login", true
	}
	if !status.SupportsBanner {
		version := status.Version
		if version == "" {
			version = "version unknown"
		}
		return fmt.Sprintf("cloudflared %s is too old for the OAuth login flow: upgrade, then run: cloudflared tunnel login", version), true
	}
	return "", false
}
