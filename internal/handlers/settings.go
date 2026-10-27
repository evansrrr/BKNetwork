package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"bknetwork/internal/events"
	appsettings "bknetwork/internal/settings"
)

// settingsSnapshot is the wire format of /api/v1/settings.
type settingsSnapshot struct {
	AutoStart           bool   `json:"autoStart"`
	SilentStart         bool   `json:"silentStart"`
	WarpAutoStart       bool   `json:"warpAutoStart"`
	WarpAppAutoStart    bool   `json:"warpAppAutoStart"`
	DisableOnlineChecks bool   `json:"disableOnlineChecks"`
	ProbeTarget         string `json:"probeTarget,omitempty"`
}

func (s settingsSnapshot) toSettings() appsettings.Settings {
	return appsettings.Settings{
		AutoStart:           s.AutoStart,
		SilentStart:         s.SilentStart,
		WarpAutoStart:       s.WarpAutoStart,
		WarpAppAutoStart:    s.WarpAppAutoStart,
		DisableOnlineChecks: s.DisableOnlineChecks,
		ProbeTarget:         strings.TrimSpace(s.ProbeTarget),
	}
}

func snapshotOf(cfg appsettings.Settings) settingsSnapshot {
	return settingsSnapshot{
		AutoStart:           cfg.AutoStart,
		SilentStart:         cfg.SilentStart,
		WarpAutoStart:       cfg.WarpAutoStart,
		WarpAppAutoStart:    cfg.WarpAppAutoStart,
		DisableOnlineChecks: cfg.DisableOnlineChecks,
		ProbeTarget:         cfg.ProbeTarget,
	}
}

// settingsSideEffects are the OS-level parts of a settings change. They are
// injected so the ordering and the rollback can be tested without writing to
// the registry or registering a scheduled task.
type settingsSideEffects struct {
	applyStartupShortcut        func(bool) error
	applyWarpAppStartupShortcut func(bool) error
}

var defaultSettingsSideEffects = settingsSideEffects{
	applyStartupShortcut:        appsettings.ApplyStartupShortcut,
	applyWarpAppStartupShortcut: appsettings.ApplyWarpAppStartupShortcut,
}

type settingsDeps struct {
	load func() (appsettings.Settings, error)
	save func(appsettings.Settings) error
}

var defaultSettingsDeps = settingsDeps{
	load: appsettings.Load,
	save: appsettings.Save,
}

// settingsChangeResult reports what a successful change actually did.
type settingsChangeResult struct {
	Applied []string
	Warning string
}

// applySettingsChange applies the OS side effects first and only then persists
// the file. If persisting fails, everything that was already applied is
// reverted, so the stored settings and the system never disagree.
func applySettingsChange(prev, next appsettings.Settings, effects settingsSideEffects, save func(appsettings.Settings) error) (settingsChangeResult, error) {
	var result settingsChangeResult
	undo := make([]func() error, 0, 2)

	if prev.AutoStart != next.AutoStart {
		if err := effects.applyStartupShortcut(next.AutoStart); err != nil {
			return result, fmt.Errorf("apply autostart: %w", err)
		}
		result.Applied = append(result.Applied, "autoStart")
		undo = append(undo, func() error { return effects.applyStartupShortcut(prev.AutoStart) })
	}

	if prev.WarpAppAutoStart != next.WarpAppAutoStart {
		if err := effects.applyWarpAppStartupShortcut(next.WarpAppAutoStart); err != nil {
			revertSettings(undo)
			return result, fmt.Errorf("apply warp autostart: %w", err)
		}
		result.Applied = append(result.Applied, "warpAppAutoStart")
		undo = append(undo, func() error { return effects.applyWarpAppStartupShortcut(prev.WarpAppAutoStart) })
	}

	if err := save(next); err != nil {
		revertSettings(undo)
		return result, fmt.Errorf("save settings: %w", err)
	}
	return result, nil
}

func revertSettings(undo []func() error) {
	for i := len(undo) - 1; i >= 0; i-- {
		_ = undo[i]()
	}
}

// settingsGetResponse keeps the settings at the top level (the UI reads
// data.autoStart) and adds an optional warning when the stored file could not
// be parsed.
type settingsGetResponse struct {
	settingsSnapshot
	Warning string `json:"warning,omitempty"`
}

func SettingsHandler(hub *events.Hub) http.HandlerFunc {
	return settingsHandler(hub, defaultSettingsDeps, defaultSettingsSideEffects)
}

func settingsHandler(hub *events.Hub, deps settingsDeps, effects settingsSideEffects) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, err := deps.load()
			response := settingsGetResponse{settingsSnapshot: snapshotOf(cfg)}
			if err != nil {
				// A corrupt file must not lock the user out of the settings
				// panel: report it and show defaults, so saving repairs the file.
				response = settingsGetResponse{Warning: err.Error()}
			}
			writeJSON(w, response, http.StatusOK)

		case http.MethodPost:
			var payload settingsSnapshot
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
				return
			}

			prevCfg, loadErr := deps.load()
			if loadErr != nil {
				// Same recovery path as GET: start from defaults so the user can
				// overwrite a damaged file instead of being stuck.
				prevCfg = appsettings.Settings{}
			}

			next := payload.toSettings()
			if next.ProbeTarget != "" && !validProbeTarget(next.ProbeTarget) {
				writeJSON(w, map[string]string{
					"error":  "invalid probeTarget",
					"detail": "probeTarget must be host:port, e.g. cloudflare.com:443",
				}, http.StatusBadRequest)
				return
			}
			result, err := applySettingsChange(prevCfg, next, effects, deps.save)
			if err != nil {
				audit(r, "settings", map[string]string{"ok": "false", "detail": err.Error()})
				notify(hub, "settings.error", "failed to update settings", map[string]any{"detail": err.Error()})
				writeJSON(w, map[string]any{"error": "settings update failed", "detail": err.Error()}, http.StatusInternalServerError)
				return
			}

			if loadErr != nil {
				result.Warning = loadErr.Error()
			}
			applied := strings.Join(result.Applied, ",")
			if applied == "" {
				applied = "none"
			}
			audit(r, "settings", map[string]string{
				"applied":             applied,
				"autoStart":           strconv.FormatBool(next.AutoStart),
				"silentStart":         strconv.FormatBool(next.SilentStart),
				"warpAutoStart":       strconv.FormatBool(next.WarpAutoStart),
				"warpAppAutoStart":    strconv.FormatBool(next.WarpAppAutoStart),
				"disableOnlineChecks": strconv.FormatBool(next.DisableOnlineChecks),
				"probeTarget":         next.ProbeTarget,
				"ok":                  "true",
			})
			notify(hub, "settings.ok", "settings updated", snapshotOf(next))
			writeJSON(w, map[string]any{
				"ok":       true,
				"settings": snapshotOf(next),
				"applied":  result.Applied,
				"warning":  result.Warning,
			}, http.StatusOK)

		default:
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
		}
	}
}
