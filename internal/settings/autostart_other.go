//go:build !windows && !darwin

package settings

func ApplyStartupShortcut(enabled bool) error {
	return nil
}

func ApplyWarpAppStartupShortcut(enabled bool) error {
	return nil
}
