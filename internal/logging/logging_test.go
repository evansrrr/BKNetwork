package logging

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotatingWriterRotatesAtSizeLimit(t *testing.T) {
	dir := t.TempDir()
	w, err := newRotatingWriter(dir, 32)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	line := strings.Repeat("a", 30) + "\n"
	if _, err := w.Write([]byte(line)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// Second write crosses the limit and must rotate first.
	if _, err := w.Write([]byte("second\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}

	day := time.Now().Format(timeLayout)
	current := filepath.Join(dir, filePrefix+day+".log")
	rotated := current + ".1"

	currentData, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if string(currentData) != "second\n" {
		t.Fatalf("current log = %q, want the new line only", currentData)
	}

	rotatedData, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("read rotated log: %v", err)
	}
	if string(rotatedData) != line {
		t.Fatalf("rotated log = %q, want the first line", rotatedData)
	}
}

func TestRecentReturnsChronologicalTail(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKNETWORK_LOG_DIR", dir)

	day := time.Now().Format(timeLayout)
	path := filepath.Join(dir, filePrefix+day+".log")
	var builder strings.Builder
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&builder, "line-%d\n", i)
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	lines := Recent(3)
	if len(lines) != 3 {
		t.Fatalf("Recent(3) returned %d lines: %v", len(lines), lines)
	}
	want := []string{"line-3", "line-4", "line-5"}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("Recent(3) = %v, want %v", lines, want)
		}
	}
}

func TestRecentReadsRotatedFileForExtraLines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKNETWORK_LOG_DIR", dir)

	day := time.Now().Format(timeLayout)
	current := filepath.Join(dir, filePrefix+day+".log")
	rotated := current + ".1"
	if err := os.WriteFile(rotated, []byte("old-1\nold-2\n"), 0o600); err != nil {
		t.Fatalf("write rotated log: %v", err)
	}
	if err := os.WriteFile(current, []byte("new-1\n"), 0o600); err != nil {
		t.Fatalf("write current log: %v", err)
	}

	lines := Recent(2)
	want := []string{"old-2", "new-1"}
	if len(lines) != len(want) {
		t.Fatalf("Recent(2) = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("Recent(2) = %v, want %v", lines, want)
		}
	}
}

func TestRecentWithoutDirectory(t *testing.T) {
	t.Setenv("BKNETWORK_LOG_DIR", filepath.Join(t.TempDir(), "missing"))
	if lines := Recent(5); lines != nil {
		t.Fatalf("Recent on a missing directory = %v, want nil", lines)
	}
	if lines := Recent(0); lines != nil {
		t.Fatalf("Recent(0) = %v, want nil", lines)
	}
}

func TestAuditFormat(t *testing.T) {
	var buffer bytes.Buffer
	original := log.Writer()
	log.SetOutput(&buffer)
	defer log.SetOutput(original)

	Audit("switch", map[string]string{
		"mode":      "ipv6",
		"ifName":    "Wi-Fi 2",
		"ok":        "true",
		"requestId": "abc123",
	})

	line := strings.TrimSpace(buffer.String())
	if !strings.Contains(line, "AUDIT action=switch") {
		t.Fatalf("audit line = %q, want the action", line)
	}
	if !strings.Contains(line, `ifName="Wi-Fi 2"`) {
		t.Fatalf("audit line = %q, want the adapter name quoted", line)
	}
	if !strings.Contains(line, "mode=ipv6") || !strings.Contains(line, "ok=true") || !strings.Contains(line, "requestId=abc123") {
		t.Fatalf("audit line = %q, want every field", line)
	}
	// Fields are sorted so two lines for the same event are comparable.
	if strings.Index(line, "ifName") > strings.Index(line, "mode") {
		t.Fatalf("audit line = %q, want keys sorted", line)
	}
}

func TestDirHonoursEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BKNETWORK_LOG_DIR", dir)
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != dir {
		t.Fatalf("Dir() = %q, want %q", got, dir)
	}
}

func TestQuoteIfNeeded(t *testing.T) {
	tests := map[string]string{
		"":         `""`,
		"WiFi":     "WiFi",
		"Wi-Fi 2":  `"Wi-Fi 2"`,
		"a\tb":     `"a\tb"`,
		`quote"me`: `"quote\"me"`,
	}
	for input, want := range tests {
		if got := quoteIfNeeded(input); got != want {
			t.Errorf("quoteIfNeeded(%q) = %q, want %q", input, got, want)
		}
	}
}
