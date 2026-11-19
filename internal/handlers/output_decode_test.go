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

func TestDecodeCommandOutputUTF16LEWithBOM(t *testing.T) {
	encoded := utf16.Encode([]rune(`{"Name":"以太网 5"}`))
	raw := make([]byte, 2+len(encoded)*2)
	raw[0], raw[1] = 0xFF, 0xFE
	for i, unit := range encoded {
		raw[2+i*2] = byte(unit)
		raw[3+i*2] = byte(unit >> 8)
	}
	if got := decodeCommandOutput(raw); got != `{"Name":"以太网 5"}` {
		t.Fatalf("decodeCommandOutput() = %q", got)
	}
}

// A BOM-less UTF-16 buffer has NUL in every high byte, which is legal UTF-8, so
// it must be detected before the UTF-8 validity check.
func TestDecodeCommandOutputUTF16LEWithoutBOM(t *testing.T) {
	const text = `{"Name":"WiFi","Status":"Up"}`
	raw := make([]byte, 0, len(text)*2)
	for _, r := range text {
		raw = append(raw, byte(r), 0)
	}
	if got := decodeCommandOutput(raw); got != text {
		t.Fatalf("decodeCommandOutput() = %q, want %q", got, text)
	}
}

func TestDecodeCommandOutputGBKEthernet(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("code page decoding uses the Windows MultiByteToWideChar API")
	}
	// GBK for 以太网 5. Read as UTF-8 this becomes the replacement-character
	// name that makes Get-NetAdapter report adapter-not-found. The code page is
	// explicit so the result depends only on these bytes, not on the host's OEM
	// code page (which may be 65001 on a system with the UTF-8 beta enabled).
	raw := []byte{0xD2, 0xD4, 0xCC, 0xAB, 0xCD, 0xF8, ' ', '5'}
	got := decodeCommandOutputWithCodePage(raw, 936)
	if got != "以太网 5" {
		t.Fatalf("decodeCommandOutputWithCodePage(936) = %q, want 以太网 5", got)
	}
}

// The GBK bytes of 已连接/以太网 must not be mistaken for UTF-16 LE, otherwise
// they would be decoded into CJK garbage instead of a legacy code page.
func TestLooksLikeUTF16LERejectsGBK(t *testing.T) {
	raw := []byte{0xD2, 0xD4, 0xCC, 0xAB, 0xCD, 0xF8, ' ', '5', 'x', 'x'}
	if looksLikeUTF16LE(raw) {
		t.Fatal("GBK adapter name was treated as UTF-16")
	}
}

func TestLooksLikeUTF16LERejectsUTF8(t *testing.T) {
	raw := []byte("Ethernet 4 is connected")
	if looksLikeUTF16LE(raw) {
		t.Fatal("ASCII UTF-8 was treated as UTF-16")
	}
}
