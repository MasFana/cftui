package health

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProbeHTTPOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method = %s, want HEAD", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	got := ProbeHTTP(context.Background(), server.URL)
	if !got.OK || got.StatusCode != http.StatusOK {
		t.Fatalf("ProbeHTTP() = %+v", got)
	}
	if !strings.Contains(got.Status(), "200 OK") {
		t.Fatalf("status = %q", got.Status())
	}
}

func TestProbeHTTPErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	got := ProbeHTTP(context.Background(), server.URL)
	if got.OK || got.StatusCode != http.StatusBadGateway || got.Err != nil {
		t.Fatalf("ProbeHTTP() = %+v", got)
	}
	if !strings.Contains(got.Status(), "502 Bad Gateway") {
		t.Fatalf("status = %q", got.Status())
	}
}

func TestProbeHTTPUnreachable(t *testing.T) {
	got := ProbeHTTP(context.Background(), "http://127.0.0.1:1")
	if got.OK || got.Err == nil || got.Status() != "○ unreachable" {
		t.Fatalf("ProbeHTTP() = %+v, status = %q", got, got.Status())
	}
}

func TestHTTPResultStatusCodeAndStatusRenderer(t *testing.T) {
	got := HTTPResult{StatusCode: http.StatusOK, OK: true}
	if got.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d", got.StatusCode)
	}
	if !strings.Contains(got.Status(), "● 200 OK") {
		t.Fatalf("Status() = %q", got.Status())
	}
}

func TestProbeServiceSelectsTransport(t *testing.T) {
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer httpServer.Close()

	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpListener.Close()
	go func() {
		for {
			conn, err := tcpListener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	port := tcpListener.Addr().(*net.TCPAddr).Port
	for _, test := range []struct {
		name    string
		service string
		ok      bool
	}{
		{"http url", httpServer.URL, true},
		{"https url", strings.Replace(httpServer.URL, "http://", "https://", 1), false},
		{"tcp", "tcp://127.0.0.1:" + strconv.Itoa(port), true},
		{"bare", "127.0.0.1:" + strconv.Itoa(port), true},
		{"synthetic", "http_status:404", true},
		{"garbage", "garbage", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ProbeService(context.Background(), test.service); got.OK != test.ok {
				t.Fatalf("ProbeService(%q) = %+v", test.service, got)
			}
		})
	}
}

func TestPublicURL(t *testing.T) {
	if got := PublicURL("app.example.com"); got != "https://app.example.com" {
		t.Fatalf("PublicURL() = %q", got)
	}
	if got := PublicURL(""); got != "" {
		t.Fatalf("PublicURL(\"\") = %q", got)
	}
}

func TestHTTPProbeNotRunByDefault(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	bytesRead := make(chan int, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		buf := make([]byte, 1)
		n, _ := conn.Read(buf)
		bytesRead <- n
	}()

	service := "tcp://" + listener.Addr().String()
	if got := ProbeService(context.Background(), service); !got.OK {
		t.Fatalf("ProbeService() = %+v", got)
	}
	if got := <-bytesRead; got != 0 {
		t.Fatalf("TCP probe wrote %d HTTP request bytes", got)
	}
}
