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

func TestServerAddrReflectsConfiguredAddressBeforeStart(t *testing.T) {
	server := NewServer("")
	if got := server.Addr(); got != DefaultAddr {
		t.Fatalf("Addr() = %q, want %q", got, DefaultAddr)
	}
}
