package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"bknetwork/internal/buildinfo"
	"bknetwork/internal/events"

	"github.com/gorilla/websocket"
)

// newWSUpgrader accepts the origins the server actually listens on. It used to
// be a package level upgrader with a hard coded ":13335", so a different listen
// port rejected every browser handshake; an empty Origin (non-browser client) is
// still allowed, and the guard rejects non-loopback hosts independently.
func newWSUpgrader(allowedOrigins []string) websocket.Upgrader {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			_, ok := allowed[origin]
			return ok
		},
	}
}

const (
	// timeoutDial bounds the connectivity probe inside the network snapshot.
	// Every request used to wait for the slowest sub-probe, so a blocked
	// cloudflare.com:443 dial cost the whole dashboard ~3s on first load.
	timeoutDial   = 1200 * time.Millisecond
	timeoutShort  = 5 * time.Second
	timeoutMedium = 8 * time.Second
	timeoutApply  = 12 * time.Second
	timeoutLong   = 15 * time.Second
)

type apiResponse struct {
	OK      bool        `json:"ok"`
	Error   string      `json:"error,omitempty"`
	Detail  string      `json:"detail,omitempty"`
	Output  string      `json:"output,omitempty"`
	Payload interface{} `json:"payload,omitempty"`
}

func writeJSON(w http.ResponseWriter, v interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

func decodeJSONList[T any](raw string) ([]T, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []T{}, nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []T
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var one T
	if err := json.Unmarshal([]byte(raw), &one); err != nil {
		return nil, err
	}
	return []T{one}, nil
}

func execWithTimeout(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func notify(hub *events.Hub, typ, msg string, data interface{}) {
	if hub == nil {
		return
	}
	hub.Publish(events.Event{Type: typ, Message: msg, Data: data})
}

func requireAdmin(w http.ResponseWriter, hub *events.Hub, eventType string, payload interface{}) bool {
	ok, adminErr := isAdmin()
	if adminErr != nil {
		log.Printf("%s: isAdmin check failed: %v", eventType, adminErr)
	}
	if !ok {
		writeJSON(w, map[string]string{"error": "admin required"}, http.StatusForbidden)
		notify(hub, eventType+".error", "administrator privilege required", payload)
		return false
	}
	return true
}

func SwitchStackHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			IfName string `json:"ifName"`
			Mode   string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
			notify(hub, "switch.error", "invalid request body", nil)
			return
		}

		if !requireAdmin(w, hub, "switch", payload) {
			return
		}

		if payload.Mode != "ipv4" && payload.Mode != "ipv6" && payload.Mode != "both" {
			writeJSON(w, map[string]string{"error": "unknown mode"}, http.StatusBadRequest)
			notify(hub, "switch.error", "unknown mode", payload)
			return
		}

		switchRes, out, err := applyNetworkMode(payload.IfName, payload.Mode)
		// The bindings changed (or were rolled back), so the cached snapshot is
		// stale for the status poll that follows this request.
		invalidateNetworkSnapshot()
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errAdapterNotFound) || errors.Is(err, errUnknownMode) {
				status = http.StatusBadRequest
			}
			result := map[string]interface{}{
				"error":  "command failed",
				"detail": err.Error(),
				"output": out,
			}
			rolledBack := false
			if switchRes != nil {
				rolledBack = switchRes.RolledBack
				result["code"] = switchRes.Code
				result["was"] = switchRes.Was
				result["now"] = switchRes.Now
				result["rolledBack"] = switchRes.RolledBack
			}
			writeJSON(w, result, status)
			notify(hub, "switch.error", "failed to switch network stack", map[string]interface{}{
				"request":    payload,
				"detail":     err.Error(),
				"output":     strings.TrimSpace(out),
				"rolledBack": rolledBack,
			})
			return
		}

		result := map[string]interface{}{"ok": true, "output": out}
		notifyData := map[string]interface{}{
			"request": payload,
			"output":  strings.TrimSpace(out),
		}
		if switchRes != nil {
			result["adapter"] = switchRes.Adapter
			result["was"] = switchRes.Was
			result["now"] = switchRes.Now
			result["changed"] = switchRes.Changed
			notifyData["was"] = switchRes.Was
			notifyData["now"] = switchRes.Now
			notifyData["changed"] = switchRes.Changed
		}
		writeJSON(w, result, http.StatusOK)
		notify(hub, "switch.ok", "network stack updated", notifyData)
	}
}

func WarpHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
			notify(hub, "warp.error", "invalid request body", nil)
			return
		}

		if !requireAdmin(w, hub, "warp", payload) {
			return
		}

		if _, err := exec.LookPath(warpCLIPath()); err != nil {
			writeJSON(w, map[string]string{"error": "warp-cli not found; please install Cloudflare WARP client"}, http.StatusBadRequest)
			notify(hub, "warp.error", "warp-cli not found", nil)
			return
		}

		// Mutations keep their own budget instead of the request context: if the
		// browser goes away mid-request, killing the helper process would also
		// kill the verification/rollback that follows.
		ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
		defer cancel()

		out, err := applyWarpAction(ctx, payload.Action)
		// The connection state feeds freeFlow in the snapshot.
		invalidateNetworkSnapshot()
		if err != nil && errors.Is(err, errUnknownWarpAction) {
			writeJSON(w, map[string]string{"error": "unknown action"}, http.StatusBadRequest)
			notify(hub, "warp.error", "unknown action", payload)
			return
		}

		if err != nil {
			result := map[string]interface{}{"error": "warp error", "detail": err.Error(), "output": out}
			writeJSON(w, result, http.StatusInternalServerError)
			notify(hub, "warp.error", "failed to update warp state", map[string]interface{}{
				"request": payload,
				"detail":  err.Error(),
				"output":  out,
			})
			return
		}

		warpProbe := probeWarpStatus(ctx)

		result := map[string]interface{}{"ok": true, "output": out, "connected": warpProbe.Connected, "status": warpProbe.Status}
		writeJSON(w, result, http.StatusOK)
		notify(hub, "warp.ok", "warp state updated", map[string]interface{}{
			"request":   payload,
			"output":    strings.TrimSpace(out),
			"connected": warpProbe.Connected,
			"status":    warpProbe.Status,
		})
	}
}

func WarpStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}
		// Read-only probe: bind it to the request so a client that navigates
		// away does not leave a warp-cli process behind.
		ctx, cancel := context.WithTimeout(r.Context(), timeoutShort)
		defer cancel()
		probe := probeWarpStatus(ctx)
		writeJSON(w, map[string]interface{}{
			"connected": probe.Connected,
			"status":    probe.Status,
		}, http.StatusOK)
	}
}

func DnsHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			IfName      string    `json:"ifName"`
			IPv4Servers *[]string `json:"ipv4Servers"`
			IPv6Servers *[]string `json:"ipv6Servers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
			notify(hub, "dns.error", "invalid request body", nil)
			return
		}

		if !requireAdmin(w, hub, "dns", payload) {
			return
		}

		if strings.TrimSpace(payload.IfName) == "" {
			writeJSON(w, map[string]string{"error": "missing ifName"}, http.StatusBadRequest)
			notify(hub, "dns.error", "missing ifName", payload)
			return
		}
		if payload.IPv4Servers == nil && payload.IPv6Servers == nil {
			writeJSON(w, map[string]string{"error": "no dns changes provided"}, http.StatusBadRequest)
			notify(hub, "dns.error", "no dns changes provided", payload)
			return
		}

		out, err := applyDnsServers(payload.IfName, payload.IPv4Servers, payload.IPv6Servers)
		invalidateNetworkSnapshot()
		if err != nil {
			writeJSON(w, map[string]interface{}{"error": "command failed", "detail": err.Error(), "output": out}, http.StatusInternalServerError)
			notify(hub, "dns.error", "failed to update dns servers", map[string]interface{}{
				"request": payload,
				"detail":  err.Error(),
				"output":  out,
			})
			return
		}

		writeJSON(w, map[string]interface{}{"ok": true, "output": out}, http.StatusOK)
		notify(hub, "dns.ok", "dns servers updated", map[string]interface{}{
			"request": payload,
			"output":  strings.TrimSpace(out),
		})
	}
}

// WebSocket keepalive parameters. The UI never sends messages, so without a
// keepalive a dead peer (sleep, network change, killed webview) is never
// noticed and the handler leaks until the process exits.
const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = 25 * time.Second
	wsReadLimit  = 1024
)

type wsKeepalive struct {
	writeWait  time.Duration
	pongWait   time.Duration
	pingPeriod time.Duration
}

var defaultWSKeepalive = wsKeepalive{
	writeWait:  wsWriteWait,
	pongWait:   wsPongWait,
	pingPeriod: wsPingPeriod,
}

func WSHandler(hub *events.Hub, allowedOrigins []string) http.HandlerFunc {
	// The cached snapshot is shared with /api/v1/status, so opening a window
	// does not start a second full collection.
	return wsHandler(hub, getNetworkSnapshot, defaultWSKeepalive, newWSUpgrader(allowedOrigins))
}

func wsHandler(hub *events.Hub, snapshot func() (networkSnapshot, error), keepalive wsKeepalive, upgrader websocket.Upgrader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("ws upgrade error:", err)
			return
		}
		defer c.Close()

		sub := hub.Subscribe(8)
		defer hub.Unsubscribe(sub)

		// Read pump: the UI sends nothing, but reading is what lets the
		// connection observe pongs and disconnects. Closing the channel unblocks
		// the write loop below.
		done := make(chan struct{})
		var closeOnce sync.Once
		stop := func() { closeOnce.Do(func() { close(done) }) }

		c.SetReadLimit(wsReadLimit)
		if err := c.SetReadDeadline(time.Now().Add(keepalive.pongWait)); err != nil {
			log.Printf("ws set read deadline: %v", err)
			return
		}
		c.SetPongHandler(func(string) error {
			return c.SetReadDeadline(time.Now().Add(keepalive.pongWait))
		})

		go func() {
			defer stop()
			for {
				if _, _, readErr := c.ReadMessage(); readErr != nil {
					return
				}
			}
		}()

		writeEvent := func(event events.Event) error {
			if err := c.SetWriteDeadline(time.Now().Add(keepalive.writeWait)); err != nil {
				return err
			}
			return c.WriteJSON(event)
		}

		if err := writeEvent(events.Event{Type: "hello", Message: "connected to BKNetwork"}); err != nil {
			log.Printf("ws write hello: %v", err)
			return
		}
		snap, snapErr := snapshot()
		if snapErr != nil {
			log.Printf("ws collect snapshot: %v", snapErr)
		}
		if err := writeEvent(events.Event{Type: "network.status", Message: "network snapshot", Data: snap}); err != nil {
			log.Printf("ws write snapshot: %v", err)
			return
		}

		ping := time.NewTicker(keepalive.pingPeriod)
		defer ping.Stop()

		for {
			select {
			case <-done:
				return
			case <-ping.C:
				if err := c.WriteControl(websocket.PingMessage, nil, time.Now().Add(keepalive.writeWait)); err != nil {
					log.Printf("ws ping: %v", err)
					return
				}
			case event, ok := <-sub:
				if !ok {
					return
				}
				if err := writeEvent(event); err != nil {
					log.Println("ws write error:", err)
					return
				}
			}
		}
	}
}

func StatusHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		last, hasLast := hub.Snapshot()
		var lastEvent interface{}
		if hasLast {
			lastEvent = last
		}
		admin, adminErr := isAdmin()
		var adminErrMsg string
		if adminErr != nil {
			adminErrMsg = adminErr.Error()
		}
		network, _ := getNetworkSnapshot()
		writeJSON(w, map[string]interface{}{
			"service": map[string]interface{}{
				"name":    "BKNetwork",
				"version": buildinfo.Version,
			},
			"admin":      admin,
			"adminError": adminErrMsg,
			"connection": map[string]interface{}{
				"websocket": "/ws",
			},
			"lastEvent": lastEvent,
			"network":   network,
			"time":      time.Now().Format(time.RFC3339),
		}, http.StatusOK)
	}
}

func LatestVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeoutMedium)
		defer cancel()

		tag, err := fetchLatestReleaseTag(ctx)
		if err != nil {
			log.Printf("latest version check failed: %v", err)
			writeJSON(w, map[string]interface{}{"ok": true, "tag": ""}, http.StatusOK)
			return
		}

		writeJSON(w, map[string]interface{}{"ok": true, "tag": tag}, http.StatusOK)
	}
}
