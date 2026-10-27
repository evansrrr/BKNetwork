//go:build windows

package handlers

import (
	"os/exec"
	"strings"
	"testing"
)

// TestEscapedValueSurvivesPowerShellRoundTrip runs the escaped value through a
// real single-quoted string and compares the output with the input. This is the
// property that matters: adapter names containing $ or ` must be passed through
// unchanged, and quotes must not break out of the literal.
func TestEscapedValueSurvivesPowerShellRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("powershell not available")
	}

	values := []string{
		"WiFi",
		"Kid's Wi-Fi",
		"以太网 2",
		"IPv6$",
		"a`b",
		"$('nested')",
		"quote'and$and`mixed",
	}

	for _, value := range values {
		script := "Write-Output '" + escapePowerShellSingleQuotedString(value) + "'"
		out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
		if err != nil {
			t.Fatalf("value %q: powershell failed: %v", value, err)
		}
		got := strings.TrimRight(string(out), "\r\n")
		if got != value {
			t.Errorf("value %q round-tripped as %q", value, got)
		}
	}
}

func TestEscapePowerShellSingleQuotedString(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"WiFi", "WiFi"},
		{"Kid's", "Kid''s"},
		{"$env:PATH", "$env:PATH"},
		{"a`b", "a`b"},
		{"'$`", "''$`"},
	}
	for _, tc := range tests {
		if got := escapePowerShellSingleQuotedString(tc.value); got != tc.want {
			t.Errorf("escapePowerShellSingleQuotedString(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestPsSingleQuote(t *testing.T) {
	if got, want := psSingleQuote("Kid's"), "'Kid''s'"; got != want {
		t.Fatalf("psSingleQuote() = %q, want %q", got, want)
	}
}
