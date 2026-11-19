//go:build !windows

package handlers

// decodeMBCSWithCodePage is a no-op on platforms without the Windows code page
// API. Their commands already write UTF-8, so decodeCommandOutput returns the
// buffer unchanged.
func decodeMBCSWithCodePage(uintptr, []byte) (string, bool) {
	return "", false
}
