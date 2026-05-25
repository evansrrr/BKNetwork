//go:build !windows

package settings

func ApplyStartupShortcut(enabled bool) error {
	return nil
}
