package cf

import "fmt"

type CapabilityErrorKind int

const (
	CapUnknown CapabilityErrorKind = iota
	CapNoAccess
	CapExpired
	CapRateLimited
	CapZoneNotInAccount
)

type CapabilityError struct {
	Kind   CapabilityErrorKind
	Op     string
	Remedy string
	cause  error
}

func (e *CapabilityError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("cloudflare %s failed: %s", e.Op, e.Remedy)
}

func (e *CapabilityError) Unwrap() error { return e.cause }

// ClassifyStatus converts HTTP response statuses into safe user remedies.
func ClassifyStatus(op string, status int) *CapabilityError {
	if status == 0 || status >= 200 && status < 300 {
		return nil
	}
	e := &CapabilityError{Kind: CapUnknown, Op: op, Remedy: "Cloudflare request failed; retry or inspect Cloudflare status"}
	switch status {
	case 401:
		e.Kind = CapExpired
		e.Remedy = "cloudflared login has expired; run: cloudflared tunnel login"
	case 403:
		e.Kind = CapNoAccess
		e.Remedy = fmt.Sprintf("cloudflared login lacks %s; update it in the Cloudflare Zero Trust dashboard, then run: cloudflared tunnel login", RemedyScope(op))
	case 429:
		e.Kind = CapRateLimited
		e.Remedy = "Cloudflare rate limit reached; wait before retrying"
	}
	return e
}

// RemedyScope returns the OAuth scope required for an operation.
func RemedyScope(op string) string {
	switch op {
	case "SyncDNS", "ListDNSRecords":
		return "Zone:DNS:Edit"
	case "ListTunnels", "GetTunnelConfig", "Connections":
		return "Account:Cloudflare Tunnel:Read"
	case "PutTunnelConfig", "CreateTunnel", "DeleteTunnel":
		return "Account:Tunnel:Edit"
	default:
		return "Account:Cloudflare Tunnel:Read"
	}
}
