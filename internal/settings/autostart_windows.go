//go:build windows

package settings

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	autostartRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValueName    = "BKNetwork"
	warpAutoStartName     = "Cloudflare WARP"
	warpAppExePath        = `C:\Program Files\Cloudflare\Cloudflare WARP\Cloudflare WARP.exe`
)

func ApplyStartupShortcut(enabled bool) error {
	return applyStartupShortcut(autostartValueName, currentExecutableCommand(), enabled)
}

func ApplyWarpAppStartupShortcut(enabled bool) error {
	return applyStartupShortcut(warpAutoStartName, fmt.Sprintf("%q", warpAppExePath), enabled)
}

func applyStartupShortcut(valueName, command string, enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRegistryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enabled {
		if err := key.DeleteValue(valueName); err != nil && !isMissingValueError(err) {
			return err
		}
		return nil
	}

	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("empty startup command")
	}
	return key.SetStringValue(valueName, command)
}

func currentExecutableCommand() string {
	exePath, err := os.Executable()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return ""
	}
	return fmt.Sprintf("%q", exePath)
}

func isMissingValueError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cannot find the file specified") || strings.Contains(msg, "the system was unable to find the specified registry key or value")
}
