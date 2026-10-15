// Package cred discovers and decodes the local cloudflared origin certificate
// (cert.pem), which embeds an API token plus the account and zone it was issued
// for. The token and private key are never logged or included in error messages.
package cred

import (
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultCertFile = "cert.pem"

	argoTunnelTokenBlock = "ARGO TUNNEL TOKEN"

	EnvOriginCert = "TUNNEL_ORIGIN_CERT"

	defaultDir = ".cloudflared"
)

var ErrMissingCredential = errors.New("cred: no cloudflared origin certificate found; run: cloudflared tunnel login")
var errIncompleteOriginCert = errors.New("incomplete origin certificate")

// ErrTokenFromEnv prevents v0.1 token configuration from being used.
var ErrTokenFromEnv = errors.New("cred: CLOUDFLARE_API_TOKEN is not supported; run: cloudflared tunnel login")

// RejectTokenFromEnv reports legacy API token configuration without reading its value.
func RejectTokenFromEnv() error {
	if os.Getenv("CLOUDFLARE_API_TOKEN") != "" {
		return ErrTokenFromEnv
	}
	return nil
}

// IncompleteCertError identifies an origin certificate that requires renewal.
type IncompleteCertError struct{ Path string }

func (e *IncompleteCertError) Error() string {
	return fmt.Sprintf("cred: origin certificate at %s is incomplete; run: cloudflared tunnel login", e.Path)
}

// OriginCert mirrors cloudflared's credentials.OriginCert JSON tags so that
// cert.pem produced by any cloudflared version decodes identically.
type OriginCert struct {
	ZoneID    string `json:"zoneID"`
	AccountID string `json:"accountID"`
	APIToken  string `json:"apiToken"`
	Endpoint  string `json:"endpoint,omitempty"`
}

// String and GoString redact the token, making OriginCert safe to log with any
// fmt verb. This is the guard behind the PRD's "secrets never logged" promise.
func (c *OriginCert) String() string {
	if c == nil {
		return "<nil origin cert>"
	}
	return fmt.Sprintf("OriginCert{zone=%s account=%s token=<redacted>}", c.ZoneID, c.AccountID)
}

func (c *OriginCert) GoString() string { return c.String() }

// FindPath applies cloudflared's own precedence: explicit flag, then
// $TUNNEL_ORIGIN_CERT, then ~/.cloudflared/cert.pem.
func FindPath(explicit string) (string, error) {
	candidates := make([]string, 0, 3)
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if env := os.Getenv(EnvOriginCert); env != "" {
		candidates = append(candidates, env)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, defaultDir, DefaultCertFile))
	}
	for _, p := range expand(candidates) {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", ErrMissingCredential
}

func expand(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if len(p) > 1 && p[0] == '~' {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[1:])
			}
		}
		out = append(out, p)
	}
	return out
}

// Decode parses a cert.pem bundle. Legacy PRIVATE KEY / CERTIFICATE blocks are
// tolerated; the ARGO TUNNEL TOKEN block must carry a non-empty zoneID and
// apiToken, matching cloudflared's decodeOriginCert exactly.
func Decode(bundle []byte) (*OriginCert, error) {
	if len(bundle) == 0 {
		return nil, errors.New("cred: cannot decode empty certificate")
	}
	var cert OriginCert
	found := false
	rest := bundle
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY", "CERTIFICATE":
		case argoTunnelTokenBlock:
			if found {
				return nil, errors.New("cred: found multiple origin tokens in the certificate")
			}
			if err := json.Unmarshal(block.Bytes, &cert); err != nil {
				return nil, fmt.Errorf("cred: parsing origin token: %w", err)
			}
			found = true
		default:
			return nil, fmt.Errorf("cred: unknown block %q in the certificate", block.Type)
		}
	}
	if !found {
		return nil, errors.New("cred: no ARGO TUNNEL TOKEN block; run: cloudflared tunnel login")
	}
	if cert.ZoneID == "" || cert.APIToken == "" {
		return nil, fmt.Errorf("cred: %w; run: cloudflared tunnel login", errIncompleteOriginCert)
	}
	return &cert, nil
}

func Load(explicitPath string) (*OriginCert, string, error) {
	path, err := FindPath(explicitPath)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, path, fmt.Errorf("cred: cannot read origin certificate at %s", path)
	}
	cert, err := Decode(raw)
	if err != nil {
		if certErrorNeedsLogin(err) {
			return nil, path, &IncompleteCertError{Path: path}
		}
		return nil, path, err
	}
	return cert, path, nil
}

func certErrorNeedsLogin(err error) bool {
	return err != nil && (errors.Is(err, errIncompleteOriginCert) || err.Error() == "cred: no ARGO TUNNEL TOKEN block; run: cloudflared tunnel login")
}
