package health

import (
	"context"
	"fmt"
	"net"
	"time"
)

type Result struct {
	Target  string
	OK      bool
	Latency time.Duration
	Err     error
}

// DefaultTimeout bounds a single probe. The add-route form probes as the user
// types, so a black-holed address must not stall the UI.
const DefaultTimeout = 1500 * time.Millisecond

func Probe(ctx context.Context, host string, port int) Result {
	target := net.JoinHostPort(host, fmt.Sprint(port))
	res := Result{Target: target}
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", target)
	res.Latency = time.Since(start)
	if err != nil {
		res.Err = err
		return res
	}
	_ = conn.Close()
	res.OK = true
	return res
}

// Status returns an icon plus text, never colour alone, so the route table
// stays readable on monochrome and colour-blind terminals.
func (r Result) Status() string {
	if r.OK {
		return fmt.Sprintf("● listening (%s)", r.Latency.Round(time.Millisecond))
	}
	return "○ not listening"
}
