//go:build darwin

package settings

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

const launchAgentName = "com.bknetwork.desktop.plist"

func ApplyStartupShortcut(enabled bool) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	launchAgentsDir := filepath.Join(filepath.Dir(configDir), "LaunchAgents")
	plistPath := filepath.Join(launchAgentsDir, launchAgentName)
	if !enabled {
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	executable := strings.TrimSpace(os.Getenv("BKNETWORK_APP_EXECUTABLE"))
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.bknetwork.desktop</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
`, html.EscapeString(executable))
	return os.WriteFile(plistPath, []byte(plist), 0o644)
}

func ApplyWarpAppStartupShortcut(bool) error {
	// Cloudflare WARP manages its own Login Item on macOS.
	return nil
}
