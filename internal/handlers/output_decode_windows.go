//go:build windows

package handlers

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var multiByteToWideChar = syscall.NewLazyDLL("kernel32.dll").NewProc("MultiByteToWideChar")

// decodeMBCSWithCodePage decodes bytes with an explicit Windows code page.
// Production passes cpOEMCP; tests pass a fixed page such as 936 (GBK) so the
// result does not depend on the machine's regional settings.
func decodeMBCSWithCodePage(codePage uintptr, b []byte) (string, bool) {
	if len(b) == 0 {
		return "", false
	}
	n, _, _ := multiByteToWideChar.Call(
		codePage,
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
		codePage,
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
