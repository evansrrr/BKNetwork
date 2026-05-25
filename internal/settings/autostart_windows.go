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
)

func ApplyStartupShortcut(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRegistryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enabled {
		if err := key.DeleteValue(autostartValueName); err != nil && !isMissingValueError(err) {
			return err
		}
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if strings.TrimSpace(exePath) == "" {
		return fmt.Errorf("empty executable path")
	}
	return key.SetStringValue(autostartValueName, fmt.Sprintf("%q", exePath))
}

func isMissingValueError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cannot find the file specified") || strings.Contains(msg, "the system was unable to find the specified registry key or value")
}
