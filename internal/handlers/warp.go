package handlers

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

var errUnknownWarpAction = errors.New("unknown warp action")

func probeWarpStatus(ctx context.Context) warpSnapshot {
	result := warpSnapshot{CheckedAt: time.Now().Format(time.RFC3339)}
	out, err := execWithTimeout(ctx, "warp-cli", "status")
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
	out, err := execWithTimeout(ctx, "warp-cli", "settings")
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
		if strings.Contains(keyLower, "status") || strings.Contains(keyLower, "state") {
			valueLower := strings.ToLower(value)
			if strings.Contains(valueLower, "connect") || strings.Contains(valueLower, "check") || strings.Contains(valueLower, "update") || strings.Contains(valueLower, "disabled") || strings.Contains(valueLower, "off") || strings.Contains(valueLower, "disconnect") {
				return false, value
			}
			return false, ""
		}
	}
	return false, ""
}

func applyWarpAction(ctx context.Context, action string) (string, error) {
	switch action {
	case "start":
		return execWithTimeout(ctx, "warp-cli", "connect")
	case "stop":
		return execWithTimeout(ctx, "warp-cli", "disconnect")
	default:
		return "", errUnknownWarpAction
	}
}

func StartWarp() error {
	if _, err := exec.LookPath("warp-cli"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()
	_, err := applyWarpAction(ctx, "start")
	return err
}
