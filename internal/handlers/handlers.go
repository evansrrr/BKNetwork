package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"bknetwork/internal/events"
	appsettings "bknetwork/internal/settings"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return origin == "http://localhost:13335" || origin == "http://127.0.0.1:13335"
	},
}

const (
	timeoutDial  = 3 * time.Second
	timeoutShort = 5 * time.Second
	timeoutMedium = 8 * time.Second
	timeoutApply = 12 * time.Second
	timeoutLong  = 15 * time.Second
)

type apiResponse struct {
	OK      bool        `json:"ok"`
	Error   string      `json:"error,omitempty"`
	Detail  string      `json:"detail,omitempty"`
	Output  string      `json:"output,omitempty"`
	Payload interface{} `json:"payload,omitempty"`
}

type settingsSnapshot struct {
	AutoStart        bool `json:"autoStart"`
	SilentStart      bool `json:"silentStart"`
	WarpAutoStart    bool `json:"warpAutoStart"`
	WarpAppAutoStart bool `json:"warpAppAutoStart"`
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
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
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

		out, err := applyNetworkMode(payload.IfName, payload.Mode)
		if err != nil {
			result := map[string]interface{}{
				"error":  "command failed",
				"detail": err.Error(),
				"output": out,
			}
			writeJSON(w, result, http.StatusInternalServerError)
			notify(hub, "switch.error", "failed to switch network stack", map[string]interface{}{
				"request": payload,
				"detail":  err.Error(),
				"output":  out,
			})
			return
		}

		result := map[string]interface{}{"ok": true, "output": out}
		writeJSON(w, result, http.StatusOK)
		notify(hub, "switch.ok", "network stack updated", map[string]interface{}{
			"request": payload,
			"output":  strings.TrimSpace(out),
		})
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

		if _, err := exec.LookPath("warp-cli"); err != nil {
			writeJSON(w, map[string]string{"error": "warp-cli not found; please install Cloudflare WARP client"}, http.StatusBadRequest)
			notify(hub, "warp.error", "warp-cli not found", nil)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
		defer cancel()

		out, err := applyWarpAction(ctx, payload.Action)
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
		ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
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

func SettingsHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, err := appsettings.Load()
			if err != nil {
				writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
				return
			}
			writeJSON(w, settingsSnapshot{AutoStart: cfg.AutoStart, SilentStart: cfg.SilentStart, WarpAutoStart: cfg.WarpAutoStart, WarpAppAutoStart: cfg.WarpAppAutoStart}, http.StatusOK)
		case http.MethodPost:
			var payload settingsSnapshot
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
				return
			}
			prevCfg, err := appsettings.Load()
			if err != nil {
				writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
				return
			}
			cfg := appsettings.Settings{AutoStart: payload.AutoStart, SilentStart: payload.SilentStart, WarpAutoStart: payload.WarpAutoStart, WarpAppAutoStart: payload.WarpAppAutoStart}
			if err := appsettings.Save(cfg); err != nil {
				writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
				return
			}
			if prevCfg.AutoStart != cfg.AutoStart {
				if err := appsettings.ApplyStartupShortcut(cfg.AutoStart); err != nil {
					writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
					return
				}
			}
			if prevCfg.WarpAppAutoStart != cfg.WarpAppAutoStart {
				if err := appsettings.ApplyWarpAppStartupShortcut(cfg.WarpAppAutoStart); err != nil {
					writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
					return
				}
			}
			notify(hub, "settings.ok", "settings updated", settingsSnapshot{AutoStart: cfg.AutoStart, SilentStart: cfg.SilentStart, WarpAutoStart: cfg.WarpAutoStart, WarpAppAutoStart: cfg.WarpAppAutoStart})
			writeJSON(w, map[string]any{"ok": true, "settings": settingsSnapshot{AutoStart: cfg.AutoStart, SilentStart: cfg.SilentStart, WarpAutoStart: cfg.WarpAutoStart, WarpAppAutoStart: cfg.WarpAppAutoStart}}, http.StatusOK)
		default:
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
		}
	}
}

func WSHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("ws upgrade error:", err)
			return
		}
		defer c.Close()

		sub := hub.Subscribe(8)
		defer hub.Unsubscribe(sub)

		if err := c.WriteJSON(events.Event{Type: "hello", Message: "connected to BKNetwork"}); err != nil {
			log.Printf("ws write hello: %v", err)
			return
		}
		snap, snapErr := collectNetworkSnapshot()
		if snapErr != nil {
			log.Printf("ws collect snapshot: %v", snapErr)
		}
		if err := c.WriteJSON(events.Event{Type: "network.status", Message: "network snapshot", Data: snap}); err != nil {
			log.Printf("ws write snapshot: %v", err)
			return
		}

		for {
			event, ok := <-sub
			if !ok {
				return
			}
			if err := c.WriteJSON(event); err != nil {
				log.Println("ws write error:", err)
				return
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
		network, _ := collectNetworkSnapshot()
		writeJSON(w, map[string]interface{}{
			"service": map[string]interface{}{
				"name":    "BKNetwork",
				"version": "dev",
			},
			"admin":      admin,
			"adminError": adminErrMsg,
			"connection": map[string]interface{}{
				"websocket": "/ws",
			},
			"lastEvent":    lastEvent,
			"network":      network,
			"time":         time.Now().Format(time.RFC3339),
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
