//go:build windows

package settings

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	autostartRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	startupApprovedRunKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	startupApprovedRun32  = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run32`
	autostartValueName    = "BKNetwork"
	warpAutoStartName     = "BKNetwork WARP"
	defaultWarpExePath    = `C:\Program Files\Cloudflare\Cloudflare WARP\Cloudflare WARP.exe`
)

var officialWarpRunValueNames = []string{"CloudflareWARP", "Cloudflare WARP"}

func ApplyStartupShortcut(enabled bool) error {
	if enabled {
		if err := removeLegacyStartupShortcut(autostartValueName); err != nil {
			return err
		}
		return applyStartupTask(autostartValueName, currentExecutablePath(), StartupNoElevateArg, enabled)
	}
	if err := applyStartupTask(autostartValueName, currentExecutablePath(), StartupNoElevateArg, enabled); err != nil {
		return err
	}
	return removeLegacyStartupShortcut(autostartValueName)
}

func ApplyWarpAppStartupShortcut(enabled bool) error {
	officialRoot, officialValueName, hasOfficial := findOfficialWarpRunValue()
	if hasOfficial {
		if err := setStartupApprovedForRunValue(officialRoot, officialValueName, enabled); err != nil {
			return err
		}
		if err := removeLegacyStartupShortcut(warpAutoStartName); err != nil {
			return err
		}
		return nil
	}
	if !enabled {
		return removeLegacyStartupShortcut(warpAutoStartName)
	}
	return applyStartupShortcut(warpAutoStartName, fmt.Sprintf("%q", resolveWarpExePath()), true)
}

func currentExecutablePath() string {
	exePath, err := os.Executable()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return ""
	}
	return exePath
}

func resolveWarpExePath() string {
	_, valueName, hasOfficial := findOfficialWarpRunValue()
	if hasOfficial {
		for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			key, err := registry.OpenKey(root, autostartRegistryPath, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			value, _, getErr := key.GetStringValue(valueName)
			key.Close()
			if getErr == nil {
				value = strings.TrimSpace(value)
				if strings.HasPrefix(value, "\"") {
					if idx := strings.Index(value[1:], "\""); idx >= 0 {
						value = value[1 : idx+1]
					}
				}
				if value != "" {
					return value
				}
			}
		}
	}
	return defaultWarpExePath
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

func removeLegacyStartupShortcut(valueName string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRegistryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if err := key.DeleteValue(valueName); err != nil && !isMissingValueError(err) {
		return err
	}
	return nil
}

func isMissingValueError(err error) bool {
	return errors.Is(err, syscall.ERROR_FILE_NOT_FOUND)
}

func findOfficialWarpRunValue() (registry.Key, string, bool) {
	if valueName, ok := findOfficialWarpRunValueInRoot(registry.LOCAL_MACHINE); ok {
		return registry.LOCAL_MACHINE, valueName, true
	}
	if valueName, ok := findOfficialWarpRunValueInRoot(registry.CURRENT_USER); ok {
		return registry.CURRENT_USER, valueName, true
	}
	return 0, "", false
}

func findOfficialWarpRunValueInRoot(root registry.Key) (string, bool) {
	key, err := registry.OpenKey(root, autostartRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer key.Close()

	for _, valueName := range officialWarpRunValueNames {
		value, _, getErr := key.GetStringValue(valueName)
		if getErr == nil && strings.TrimSpace(value) != "" {
			return valueName, true
		}
	}

	names, err := key.ReadValueNames(0)
	if err != nil {
		return "", false
	}
	warpPathLower := strings.ToLower(resolveWarpExePath())
	for _, name := range names {
		v, _, getErr := key.GetStringValue(name)
		if getErr != nil {
			continue
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(v)), warpPathLower) {
			return name, true
		}
	}
	return "", false
}

func setStartupApprovedForRunValue(root registry.Key, valueName string, enabled bool) error {
	if err := setStartupApprovedForRunValueInKey(root, startupApprovedRunKey, valueName, enabled); err != nil {
		return err
	}
	if err := setStartupApprovedForRunValueInKey(root, startupApprovedRun32, valueName, enabled); err != nil {
		return err
	}
	return nil
}

func setStartupApprovedForRunValueInKey(root registry.Key, keyPath, valueName string, enabled bool) error {
	key, _, err := registry.CreateKey(root, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	state, _, err := key.GetBinaryValue(valueName)
	if err != nil || len(state) == 0 {
		state = make([]byte, 12)
	}
	if enabled {
		state[0] = 0x02
	} else {
		state[0] = 0x03
	}
	return key.SetBinaryValue(valueName, state)
}
