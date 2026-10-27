package server

import (
	"testing"
	"time"
)

// TestServerTimeouts pins the hardening values: an unbounded server could be
// held open by a peer that never finishes its request headers.
func TestServerTimeouts(t *testing.T) {
	server := NewServer("")
	srv := server.httpServer

	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout must be set")
	}
	if srv.ReadTimeout <= 0 {
		t.Error("ReadTimeout must be set")
	}
	if srv.IdleTimeout <= 0 {
		t.Error("IdleTimeout must be set")
	}
	if srv.MaxHeaderBytes <= 0 || srv.MaxHeaderBytes > 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want a small bound", srv.MaxHeaderBytes)
	}
	// A write timeout would truncate the switch response, which reports the
	// rollback outcome. Keep it unset and bound the work per handler instead.
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %s, want 0 (handlers bound themselves)", srv.WriteTimeout)
	}
}

func TestShutdownTimeoutIsBounded(t *testing.T) {
	if shutdownTimeout <= 0 || shutdownTimeout > 30*time.Second {
		t.Fatalf("shutdownTimeout = %s, want a value between 0 and 30s", shutdownTimeout)
	}
}
