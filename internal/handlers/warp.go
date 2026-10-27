package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

var errUnknownWarpAction = errors.New("unknown warp action")

func probeWarpStatus(ctx context.Context) warpSnapshot {
	result := warpSnapshot{CheckedAt: time.Now().Format(time.RFC3339)}
	// Newer warp-cli builds accept --json and are parsed first: JSON has a
	// stable schema, while the text form has changed wording several times.
	// When the flag is unsupported (or the output is not JSON) the plain text
	// form below stays the source of truth.
	if !warpJSONUnsupported.Load() {
		jsonOut, jsonErr := execWithTimeout(ctx, warpCLIPath(), "status", "--json")
		jsonRaw := strings.TrimSpace(jsonOut)
		if jsonErr == nil && strings.HasPrefix(jsonRaw, "{") {
			if connected, status, ok := parseWarpStatusJSON(jsonRaw); ok {
				result.Raw = jsonRaw
				result.Connected, result.Status = connected, status
				return result
			}
		}
		if jsonErr != nil && strings.Contains(strings.ToLower(jsonOut), "unexpected argument") {
			warpJSONUnsupported.Store(true)
		}
	}
	out, err := execWithTimeout(ctx, warpCLIPath(), "status")
	result.Raw = strings.TrimSpace(out)
	if err != nil && result.Raw == "" {
		result.Error = err.Error()
		return result
	}
	result.Connected, result.Status = parseWarpConnected(result.Raw)
	return result
}

func probeWarpSettings(ctx context.Context) warpSettingsSnapshot {
	result := warpSettingsSnapshot{CheckedAt: time.Now().Format(time.RFC3339)}
	out, err := execWithTimeout(ctx, warpCLIPath(), "settings")
	raw := strings.TrimSpace(out)
	if err != nil && raw == "" {
		result.Error = err.Error()
		return result
	}
	result.Mode = parseWarpSettingsValue(raw, "Mode")
	result.TunnelProtocol = parseWarpSettingsValue(raw, "WARP tunnel protocol")
	return result
}

func parseWarpSettingsValue(raw, key string) string {
	needle := key + ":"
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		idx := strings.Index(line, needle)
		if idx < 0 {
			continue
		}
		value := strings.TrimSpace(line[idx+len(needle):])
		if value != "" {
			return value
		}
	}
	return ""
}

// parseWarpConnected classifies warp-cli status text. "Connected" only means
// usable connectivity when the network line agrees: warp-cli reports the
// tunnel state and the network health separately, and "Connected" with
// "Network: down" used to be shown as a working connection.
func parseWarpConnected(raw string) (bool, string) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return false, ""
	}
	if strings.Contains(text, "status update: connected") && (strings.Contains(text, "network: healthy") || strings.Contains(text, "network: unstable")) {
		return true, "Connected"
	}
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			continue
		}
		keyLower := strings.ToLower(key)
		if !strings.Contains(keyLower, "status") && !strings.Contains(keyLower, "state") {
			continue
		}
		connected, recognized := warpValueMeansConnected(value)
		if !recognized {
			// Unknown wording (localized output, a new release): keep scanning
			// instead of answering from a guess.
			continue
		}
		if connected && !warpNetworkHealthy(raw) {
			return false, value
		}
		return connected, value
	}
	return false, ""
}

// warpValueMeansConnected classifies one status word. The negatives are listed
// first because "disconnected" and "connecting" both contain "connected"'s
// substring - the old check treated every value containing "connect",
// including plain "Connected", as not connected.
func warpValueMeansConnected(value string) (connected, recognized bool) {
	valueLower := strings.ToLower(strings.TrimSpace(value))
	if valueLower == "" {
		return false, false
	}
	switch {
	case strings.Contains(valueLower, "disconnect"),
		strings.Contains(valueLower, "connecting"),
		strings.Contains(valueLower, "connect failed"),
		strings.Contains(valueLower, "check"),
		strings.Contains(valueLower, "offline"),
		strings.Contains(valueLower, "disabled"),
		strings.Contains(valueLower, "update"):
		return false, true
	case strings.Contains(valueLower, "connect"):
		return true, true
	}
	return false, false
}

// warpNetworkHealthy reports whether the output carries a network line, and if
// so whether it says the network is usable. No network information means no
// veto: plain "Status: Connected" (older warp-cli builds) must not be judged
// offline just because the health line is absent.
func warpNetworkHealthy(raw string) bool {
	network := parseWarpSettingsValue(raw, "Network")
	if network == "" {
		return true
	}
	networkLower := strings.ToLower(network)
	return strings.Contains(networkLower, "healthy") || strings.Contains(networkLower, "unstable")
}

// warpJSONUnsupported remembers that this warp-cli build rejects the --json
// flag, so later probes skip the extra process spawn instead of paying for it
// on every snapshot.
var warpJSONUnsupported atomic.Bool

// parseWarpStatusJSON reads the JSON output of newer `warp-cli status --json`
// builds. ok is false when the output is not JSON or carries no recognizable
// status field, in which case the caller falls back to the text form.
func parseWarpStatusJSON(raw string) (connected bool, status string, ok bool) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if !strings.HasPrefix(trimmed, "{") {
		return false, "", false
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
		return false, "", false
	}
	for key, value := range fields {
		keyLower := strings.ToLower(key)
		if strings.Contains(keyLower, "connected") {
			if flag, isBool := value.(bool); isBool {
				if flag {
					return true, "Connected", true
				}
				return false, "Disconnected", true
			}
		}
		if !strings.Contains(keyLower, "status") && !strings.Contains(keyLower, "state") {
			continue
		}
		text, isText := value.(string)
		if !isText {
			continue
		}
		if flag, recognized := warpValueMeansConnected(text); recognized {
			return flag, text, true
		}
	}
	return false, "", false
}

func applyWarpAction(ctx context.Context, action string) (string, error) {
	switch action {
	case "start":
		return execWithTimeout(ctx, warpCLIPath(), "connect")
	case "stop":
		return execWithTimeout(ctx, warpCLIPath(), "disconnect")
	default:
		return "", errUnknownWarpAction
	}
}

func StartWarp() error {
	if _, err := exec.LookPath(warpCLIPath()); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()
	_, err := applyWarpAction(ctx, "start")
	return err
}

func warpCLIPath() string {
	if path, err := exec.LookPath("warp-cli"); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		const appPath = "/Applications/Cloudflare WARP.app/Contents/Resources/warp-cli"
		if info, err := os.Stat(appPath); err == nil && !info.IsDir() {
			return appPath
		}
	}
	return "warp-cli"
}
