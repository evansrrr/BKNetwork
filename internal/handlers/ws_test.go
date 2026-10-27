package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bknetwork/internal/events"

	"github.com/gorilla/websocket"
)

// testWSKeepalive uses short timings so the tests do not have to wait for the
// production 25s/60s values.
var testWSKeepalive = wsKeepalive{
	writeWait:  200 * time.Millisecond,
	pongWait:   400 * time.Millisecond,
	pingPeriod: 100 * time.Millisecond,
}

// testUpgrader accepts the production origin, which is what dialWS sends.
func testUpgrader() websocket.Upgrader {
	return newWSUpgrader([]string{"http://127.0.0.1:13335", "http://localhost:13335"})
}

// dialWS connects to the test server. The origin is set to the production
// origin because the upgrader only accepts that one.
func dialWS(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	header := http.Header{"Origin": []string{"http://127.0.0.1:13335"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readEvent(t *testing.T, conn *websocket.Conn, timeout time.Duration) events.Event {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var event events.Event
	if err := conn.ReadJSON(&event); err != nil {
		t.Fatalf("read event: %v", err)
	}
	return event
}

func TestWSHandlerSendsSnapshotThenEvents(t *testing.T) {
	hub := events.NewHub()
	snapshot := func() (networkSnapshot, error) {
		return networkSnapshot{CollectedAt: "2026-10-27T00:00:00Z"}, nil
	}
	server := httptest.NewServer(wsHandler(hub, snapshot, testWSKeepalive, testUpgrader()))
	defer server.Close()

	conn := dialWS(t, server.URL)

	if event := readEvent(t, conn, 2*time.Second); event.Type != "hello" {
		t.Fatalf("first event = %q, want hello", event.Type)
	}
	if event := readEvent(t, conn, 2*time.Second); event.Type != "network.status" {
		t.Fatalf("second event = %q, want network.status", event.Type)
	}

	hub.Publish(events.Event{Type: "switch.ok", Message: "network stack updated"})
	event := readEvent(t, conn, 2*time.Second)
	if event.Type != "switch.ok" || event.Message != "network stack updated" {
		t.Fatalf("broadcast event = %+v", event)
	}
	if event.Time.IsZero() {
		t.Fatal("broadcast event has no timestamp")
	}
}

// TestWSHandlerKeepsResponsiveClientAlive makes sure the keepalive does not
// disconnect a client that answers pings. A single reader goroutine is used
// because gorilla forbids reading after a read deadline has fired.
func TestWSHandlerKeepsResponsiveClientAlive(t *testing.T) {
	hub := events.NewHub()
	server := httptest.NewServer(wsHandler(hub, func() (networkSnapshot, error) {
		return networkSnapshot{}, nil
	}, testWSKeepalive, testUpgrader()))
	defer server.Close()

	conn := dialWS(t, server.URL)
	readEvent(t, conn, 2*time.Second) // hello
	readEvent(t, conn, 2*time.Second) // snapshot

	received := make(chan events.Event, 8)
	readErr := make(chan error, 1)
	go func() {
		for {
			if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
				readErr <- err
				return
			}
			var event events.Event
			if err := conn.ReadJSON(&event); err != nil {
				readErr <- err
				return
			}
			received <- event
		}
	}()

	// Staying connected for well over pongWait means the pings were answered.
	time.Sleep(3 * testWSKeepalive.pongWait)

	hub.Publish(events.Event{Type: "warp.ok", Message: "still alive"})
	select {
	case event := <-received:
		if event.Type != "warp.ok" {
			t.Fatalf("event = %+v, want warp.ok", event)
		}
	case err := <-readErr:
		t.Fatalf("connection dropped while answering pings: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("no event received after the keepalive window")
	}
}

// TestWSHandlerDropsSilentClient covers the leak fixed here: a peer that stops
// answering (suspended machine, killed webview) used to keep the handler and
// its goroutine alive forever.
func TestWSHandlerDropsSilentClient(t *testing.T) {
	hub := events.NewHub()
	server := httptest.NewServer(wsHandler(hub, func() (networkSnapshot, error) {
		return networkSnapshot{}, nil
	}, testWSKeepalive, testUpgrader()))
	defer server.Close()

	conn := dialWS(t, server.URL)
	readEvent(t, conn, 2*time.Second) // hello
	readEvent(t, conn, 2*time.Second) // snapshot

	// Stop reading: gorilla only answers pings while reading, so the server
	// stops receiving pongs and has to close the connection.
	time.Sleep(3 * testWSKeepalive.pongWait)

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected the server to close the connection of a silent peer")
	} else if isTimeout(err) {
		t.Fatal("the server never closed the connection of a silent peer")
	}
}

func isTimeout(err error) bool {
	netErr, ok := err.(interface{ Timeout() bool })
	return ok && netErr.Timeout()
}

// TestWSHandlerRejectsForeignOrigin pins the per-server origin list: a foreign
// page must not be able to open a socket to the local API.
func TestWSHandlerRejectsForeignOrigin(t *testing.T) {
	hub := events.NewHub()
	server := httptest.NewServer(wsHandler(hub, func() (networkSnapshot, error) {
		return networkSnapshot{}, nil
	}, testWSKeepalive, testUpgrader()))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Origin": []string{"http://evil.example"}}
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected the handshake to be rejected")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		code := 0
		if response != nil {
			code = response.StatusCode
		}
		t.Fatalf("status = %d, want 403", code)
	}
}

// TestWSHandlerAcceptsConfiguredPort makes sure the origin list is not hard
// coded to 13335 any more.
func TestWSHandlerAcceptsConfiguredPort(t *testing.T) {
	hub := events.NewHub()
	upgrader := newWSUpgrader([]string{"http://127.0.0.1:61000"})
	server := httptest.NewServer(wsHandler(hub, func() (networkSnapshot, error) {
		return networkSnapshot{}, nil
	}, testWSKeepalive, upgrader))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Origin": []string{"http://127.0.0.1:61000"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("handshake for the configured port failed: %v", err)
	}
	defer conn.Close()
}
