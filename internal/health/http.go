// Package health provides on-demand origin probes. HTTP probing only runs when
// callers invoke ProbeHTTP or pass an http/https service string to ProbeService;
// Probe and non-HTTP ProbeService calls never issue HTTP requests.
package health

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxProbeBody = 4 << 10

// HTTPClient is shared by HTTP probe callers. It bounds one request and follows
// at most three redirects.
var HTTPClient = &http.Client{
	Timeout: DefaultTimeout,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// HTTPResult is the outcome of an HTTP(S) probe.
type HTTPResult struct {
	URL        string
	StatusCode int
	OK         bool
	Latency    time.Duration
	Err        error
}

// ProbeHTTP issues a HEAD request (falling back to GET when HEAD is not
// allowed) against url, honouring ctx and DefaultTimeout, following up to 3
// redirects. It never panics and never reads more than a small body.
func ProbeHTTP(ctx context.Context, rawURL string) HTTPResult {
	res := HTTPResult{URL: rawURL}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	start := time.Now()
	response, err := probeHTTP(ctx, rawURL, http.MethodHead)
	res.Latency = time.Since(start)
	if err != nil {
		res.Err = err
		return res
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, maxProbeBody)

	if response.StatusCode == http.StatusMethodNotAllowed {
		start = time.Now()
		response.Body.Close()
		response, err = probeHTTP(ctx, rawURL, http.MethodGet)
		res.Latency = time.Since(start)
		if err != nil {
			res.Err = err
			return res
		}
		defer response.Body.Close()
		_, _ = io.CopyN(io.Discard, response.Body, maxProbeBody)
	}

	res.StatusCode = response.StatusCode
	res.OK = response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusBadRequest
	return res
}

func probeHTTP(ctx context.Context, rawURL, method string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return HTTPClient.Do(req)
}

// Status renders an icon plus text plus latency, never colour alone.
func (r HTTPResult) Status() string {
	if r.Err != nil {
		return "○ unreachable"
	}
	if r.OK {
		return fmt.Sprintf("● %d %s (%s)", r.StatusCode, http.StatusText(r.StatusCode), r.Latency.Round(time.Millisecond))
	}
	return fmt.Sprintf("▲ %d %s (%s)", r.StatusCode, http.StatusText(r.StatusCode), r.Latency.Round(time.Millisecond))
}

// ProbeService probes one ingress target. HTTP(S) services use ProbeHTTP;
// tcp:// and bare host:port services use Probe. http_status:* is synthetic.
func ProbeService(ctx context.Context, service string) Result {
	if strings.HasPrefix(service, "http_status:") {
		return Result{Target: service, OK: true}
	}

	if parsed, err := url.Parse(service); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		httpResult := ProbeHTTP(ctx, service)
		result := Result{Target: service, OK: httpResult.OK, Latency: httpResult.Latency, Err: httpResult.Err}
		if !result.OK && result.Err == nil {
			result.Err = fmt.Errorf("HTTP status %d", httpResult.StatusCode)
		}
		return result
	}

	target := strings.TrimPrefix(service, "tcp://")
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return Result{Target: service, Err: fmt.Errorf("parse service %q: %w", service, err)}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Result{Target: service, Err: fmt.Errorf("parse service %q: invalid port %q", service, portText)}
	}
	return Probe(ctx, host, port)
}

// PublicURL derives the browser URL for a route hostname. Catch-all routes
// have no hostname and no public URL.
func PublicURL(hostname string) string {
	if hostname == "" {
		return ""
	}
	return "https://" + hostname
}
