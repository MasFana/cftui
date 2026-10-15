package health

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestProbeDetectsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	got := Probe(context.Background(), "127.0.0.1", port)
	if !got.OK {
		t.Fatalf("listener on %d must probe OK: %v", port, got.Err)
	}
	if !strings.Contains(got.Status(), "listening") {
		t.Fatalf("status must say listening: %q", got.Status())
	}
}

func TestProbeDetectsClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if got := Probe(context.Background(), "127.0.0.1", port); got.OK {
		t.Fatal("closed port must not probe OK")
	} else if got.Status() != "○ not listening" {
		t.Fatalf("unexpected status: %q", got.Status())
	}
}

func TestProbeRespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// 192.0.2.0/24 is TEST-NET-1, guaranteed unroutable, so without the
	// cancellation this would block until DefaultTimeout instead of returning.
	if got := Probe(ctx, "192.0.2.1", 9); got.OK {
		t.Fatal("cancelled probe must not report OK")
	}
}
