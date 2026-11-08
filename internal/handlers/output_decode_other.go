//go:build !windows

package handlers

func decodeMBCS([]byte) (string, bool) {
	return "", false
}
