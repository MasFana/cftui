package validate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var ServiceTypes = []string{
	"http", "https", "tcp", "ssh", "rdp", "smb", "unix", "http_status", "hello_world",
}

const (
	MinPort = 1
	MaxPort = 65535
)

var (
	labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	httpErr = regexp.MustCompile(`^http_status:[1-5][0-9][0-9]$`)
)

// Hostname enforces RFC-1123 labels and, when zone is non-empty, that the
// hostname sits inside it. The zone comes from the origin certificate, which
// pins cftui to a single zone.
func Hostname(host, zone string) error {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return errors.New("hostname is required")
	}
	if len(h) > 253 {
		return errors.New("hostname is longer than 253 characters")
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 {
		return fmt.Errorf("hostname %q needs at least two labels", host)
	}
	for _, l := range labels {
		if !labelRe.MatchString(l) {
			return fmt.Errorf("hostname label %q is not RFC-1123 valid", l)
		}
	}
	if zone != "" {
		z := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
		if h != z && !strings.HasSuffix(h, "."+z) {
			return fmt.Errorf("hostname %q is outside the zone %q pinned by the origin certificate", host, zone)
		}
	}
	return nil
}

func Port(p int) error {
	if p < MinPort || p > MaxPort {
		return fmt.Errorf("port %d is outside the valid range %d-%d", p, MinPort, MaxPort)
	}
	return nil
}

func PortString(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("port is required")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("port %q is not a number", s)
	}
	return Port(n)
}

// Service requires a known scheme whose address form matches it: http/https/
// tcp/ssh/rdp/smb need host:port, unix needs an absolute socket path,
// http_status needs a 3-digit code, and hello_world needs nothing.
func Service(service string) error {
	s := strings.TrimSpace(service)
	if s == "" {
		return errors.New("service is required")
	}
	if httpErr.MatchString(s) {
		return nil
	}
	if s == "hello_world" {
		return nil
	}
	if strings.HasPrefix(s, "unix:") {
		path := strings.TrimPrefix(s, "unix:")
		if path == "" || !strings.HasPrefix(path, "/") {
			return fmt.Errorf("unix service needs an absolute socket path, got %q", s)
		}
		return nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return fmt.Errorf("service %q is not a valid address: %w", s, err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("service %q must include a scheme (e.g. http://localhost:3000)", s)
	}
	if !contains(ServiceTypes, u.Scheme) {
		return fmt.Errorf("service type %q is not one of %s", u.Scheme, strings.Join(ServiceTypes, ", "))
	}
	if u.Hostname() == "" {
		return fmt.Errorf("service %q is missing a host", s)
	}
	port := u.Port()
	if port == "" {
		return fmt.Errorf("service %q is missing a port", s)
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return PortString(port)
	}
	return PortString(port)
}

func Duplicate(existing []struct{ Hostname, Path string }, hostname, path string) error {
	for _, e := range existing {
		if strings.EqualFold(e.Hostname, hostname) && strings.TrimSpace(e.Path) == strings.TrimSpace(path) {
			return fmt.Errorf("%s%s is already routed on this tunnel", hostname, path)
		}
	}
	return nil
}

func CatchAllMutable(count int) error {
	if count < 1 {
		return errors.New("the catch-all rule cannot be removed, only changed")
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
