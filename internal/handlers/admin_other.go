//go:build !windows

package handlers

import "runtime"

func isAdmin() (bool, error) {
	// On macOS, mutating commands request authorization through the native
	// administrator prompt instead of requiring the GUI process to run as root.
	return runtime.GOOS == "darwin", nil
}
