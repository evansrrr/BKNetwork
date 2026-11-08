package handlers

import (
	"runtime"
	"testing"
	"unicode/utf16"
)

func TestDecodeCommandOutputKeepsUTF8Chinese(t *testing.T) {
	in := []byte("以太网 5")
	if got := decodeCommandOutput(in); got != "以太网 5" {
		t.Fatalf("decodeCommandOutput() = %q, want 以太网 5", got)
	}
}

func TestDecodeCommandOutputUTF16LE(t *testing.T) {
	encoded := utf16.Encode([]rune(`{"Name":"以太网 5"}`))
	raw := make([]byte, 2+len(encoded)*2)
	raw[0], raw[1] = 0xFF, 0xFE
	for i, unit := range encoded {
		raw[2+i*2] = byte(unit)
		raw[3+i*2] = byte(unit >> 8)
	}
	got := decodeCommandOutput(raw)
	if got != `{"Name":"以太网 5"}` {
		t.Fatalf("decodeCommandOutput() = %q", got)
	}
}

func TestDecodeCommandOutputGBKEthernet(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("OEM code page decoding is a Windows API")
	}
	// GBK for 以太网 5. Read as UTF-8 this becomes the replacement-character
	// name seen in the diagnostics log, and Get-NetAdapter cannot find it.
	raw := []byte{0xD2, 0xD4, 0xCC, 0xAB, 0xCD, 0xF8, ' ', '5'}
	got := decodeCommandOutput(raw)
	if got != "以太网 5" {
		t.Fatalf("decodeCommandOutput() = %q, want 以太网 5", got)
	}
}

func TestLooksLikeUTF16LERejectsGBK(t *testing.T) {
	raw := []byte{0xD2, 0xD4, 0xCC, 0xAB, 0xCD, 0xF8, ' ', '5', 'x', 'x'}
	if looksLikeUTF16LE(raw) {
		t.Fatal("GBK adapter name was treated as UTF-16")
	}
}
