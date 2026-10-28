package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerReadyAfterListen(t *testing.T) {
	server := NewServer("127.0.0.1:0")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()

	select {
	case <-server.Ready():
	case err := <-done:
		t.Fatalf("server exited before becoming ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not become ready")
	}

	// The listener is accepting connections by the time Ready is closed.
	conn, err := net.DialTimeout("tcp", server.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial bound address %s: %v", server.Addr(), err)
	}
	_ = conn.Close()

	response, err := http.Get("http://" + server.Addr() + "/")
	if err != nil {
		t.Fatalf("GET on the bound address: %v", err)
	}
	defer response.Body.Close()
	// A served page proves the guard accepted the ephemeral port as loopback.
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned an error during shutdown: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Start did not return after the context was cancelled")
	}
}

func TestServerStartReportsBindError(t *testing.T) {
	// Occupy a port so the bind fails.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer occupied.Close()

	server := NewServer(occupied.Addr().String())
	err = server.Start(context.Background())
	if err == nil {
		t.Fatal("expected a bind error")
	}

	select {
	case <-server.Ready():
		t.Fatal("Ready must not be closed when the bind fails")
	default:
	}
}

// fakeAddr lets the loopback helper be tested without opening a socket.
type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func TestIsLoopbackListenAddr(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:13335", true},
		{"[::1]:13335", true},
		{"localhost:13335", true},
		{"0.0.0.0:13335", false},
		{"[::]:13335", false},
		{"192.168.1.10:13335", false},
	} {
		if got := isLoopbackListenAddr(fakeAddr(tc.addr)); got != tc.want {
			t.Errorf("isLoopbackListenAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestServerRefusesNonLoopbackBindWithoutToken(t *testing.T) {
	t.Setenv("BKNETWORK_API_TOKEN", "")
	server := NewServer("0.0.0.0:0")
	err := server.Start(context.Background())
	if err == nil {
		t.Fatal("expected a non-loopback bind without a token to be refused")
	}

	select {
	case <-server.Ready():
		t.Fatal("Ready must not be closed when the bind is refused")
	default:
	}
}

func TestServerAddrReflectsConfiguredAddressBeforeStart(t *testing.T) {
	server := NewServer("")
	if got := server.Addr(); got != DefaultAddr {
		t.Fatalf("Addr() = %q, want %q", got, DefaultAddr)
	}
}

// TestServerAddrEnvOverride covers the BKNETWORK_LISTEN_ADDR fallback: an
// explicit argument always wins, an empty environment falls back to the
// default, and a non default port must be honoured verbatim (the guard, the
// CSP and the WebSocket origins derive their port from it).
func TestServerAddrEnvOverride(t *testing.T) {
	t.Setenv("BKNETWORK_LISTEN_ADDR", "127.0.0.1:18335")
	if got := NewServer("").Addr(); got != "127.0.0.1:18335" {
		t.Fatalf("Addr() = %q, want the env override 127.0.0.1:18335", got)
	}

	t.Setenv("BKNETWORK_LISTEN_ADDR", "   ")
	if got := NewServer("").Addr(); got != DefaultAddr {
		t.Fatalf("Addr() = %q, want %q for a blank override", got, DefaultAddr)
	}

	if got := NewServer("127.0.0.1:9").Addr(); got != "127.0.0.1:9" {
		t.Fatalf("Addr() = %q, want the explicit argument to win", got)
	}
}

// TestServerServesOnEnvOverridePort starts a real listener on the overridden
// port and proves the guard accepts its own loopback origin on that port.
func TestServerServesOnEnvOverridePort(t *testing.T) {
	t.Setenv("BKNETWORK_LISTEN_ADDR", "127.0.0.1:0")
	server := NewServer("")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- server.Start(ctx) }()

	select {
	case <-server.Ready():
	case err := <-done:
		t.Fatalf("server exited before becoming ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not become ready")
	}

	response, err := http.Get("http://" + server.Addr() + "/")
	if err != nil {
		t.Fatalf("GET on the bound address: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Start returned an error during shutdown: %v", err)
	}
}
