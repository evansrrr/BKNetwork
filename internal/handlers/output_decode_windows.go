//go:build windows

package handlers

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var multiByteToWideChar = syscall.NewLazyDLL("kernel32.dll").NewProc("MultiByteToWideChar")

// CP_OEMCP asks Windows for the OEM code page. On a Chinese system that is
// 936 (GBK), which is what netsh writes when the process has no UTF-8 console.
const cpOEMCP = 1

// decodeMBCS decodes bytes from the system OEM code page.
func decodeMBCS(b []byte) (string, bool) {
	if len(b) == 0 {
		return "", false
	}
	n, _, _ := multiByteToWideChar.Call(
		uintptr(cpOEMCP),
		0,
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(len(b)),
		0,
		0,
	)
	if n == 0 {
		return "", false
	}
	units := make([]uint16, int(n))
	n, _, _ = multiByteToWideChar.Call(
		uintptr(cpOEMCP),
		0,
		uintptr(unsafe.Pointer(&b[0])),
		uintptr(len(b)),
		uintptr(unsafe.Pointer(&units[0])),
		uintptr(len(units)),
	)
	if n == 0 {
		return "", false
	}
	return string(utf16.Decode(units[:int(n)])), true
}
