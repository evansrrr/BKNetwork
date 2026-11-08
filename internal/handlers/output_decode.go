package handlers

import (
	"unicode/utf16"
	"unicode/utf8"
)

// decodeCommandOutput turns a child process's stdout into a Go string.
//
// Windows commands do not agree on an encoding. PowerShell on a UTF-8 system
// writes UTF-8, but a service process on a Chinese system still writes the OEM
// code page (GBK) for netsh and sometimes for PowerShell. Reading those bytes
// as UTF-8 turns "以太网 5" into a different string, and every later
// Get-NetAdapter lookup fails with adapter-not-found. UTF-16 is what some
// PowerShell hosts emit when stdout is a pipe; ASCII in UTF-16 is also valid
// UTF-8 because NUL is a legal byte, so it has to be recognized first.
func decodeCommandOutput(out []byte) string {
	if len(out) == 0 {
		return ""
	}
	if text, ok := decodeUTF16LE(out); ok {
		return text
	}
	if utf8.Valid(out) {
		return string(out)
	}
	if text, ok := decodeMBCS(out); ok {
		return text
	}
	return string(out)
}

// decodeUTF16LE reports whether out is UTF-16 LE text and, when it is, the
// decoded string. A BOM is decisive. Without one, the buffer has to be mostly
// ASCII code units (a JSON document or a netsh table): the high byte of those
// units is NUL. GBK Chinese does not look like that.
func decodeUTF16LE(out []byte) (string, bool) {
	if len(out) >= 2 && out[0] == 0xFF && out[1] == 0xFE {
		out = out[2:]
	} else if !looksLikeUTF16LE(out) {
		return "", false
	}
	if len(out)%2 != 0 {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "", true
	}
	units := make([]uint16, len(out)/2)
	for i := range units {
		units[i] = uint16(out[i*2]) | uint16(out[i*2+1])<<8
	}
	return string(utf16.Decode(units)), true
}

func looksLikeUTF16LE(out []byte) bool {
	if len(out) < 8 || len(out)%2 != 0 {
		return false
	}
	n := len(out) / 2
	if n > 64 {
		n = 64
	}
	zeros := 0
	for i := 0; i < n; i++ {
		if out[i*2] != 0 && out[i*2+1] == 0 {
			zeros++
		}
	}
	return zeros*4 >= n*3
}
