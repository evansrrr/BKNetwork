package handlers

import (
	"errors"
	"strings"
	"testing"
)

func TestParseNetworkMode(t *testing.T) {
	tests := []struct {
		mode     string
		wantIPv4 bool
		wantIPv6 bool
		wantErr  bool
	}{
		{"ipv4", true, false, false},
		{"ipv6", false, true, false},
		{"both", true, true, false},
		{"", false, false, true},
		{"ipv5", false, false, true},
	}
	for _, tc := range tests {
		ipv4, ipv6, err := parseNetworkMode(tc.mode)
		if (err != nil) != tc.wantErr {
			t.Fatalf("parseNetworkMode(%q) error = %v, wantErr %v", tc.mode, err, tc.wantErr)
		}
		if tc.wantErr {
			if !errors.Is(err, errUnknownMode) {
				t.Fatalf("parseNetworkMode(%q) error = %v, want errUnknownMode", tc.mode, err)
			}
			continue
		}
		if ipv4 != tc.wantIPv4 || ipv6 != tc.wantIPv6 {
			t.Fatalf("parseNetworkMode(%q) = (%v,%v), want (%v,%v)", tc.mode, ipv4, ipv6, tc.wantIPv4, tc.wantIPv6)
		}
	}
}

func TestBuildSwitchScriptRequestsDesiredState(t *testing.T) {
	tests := []struct {
		name     string
		wantIPv4 bool
		wantIPv6 bool
	}{
		{"ipv4 only", true, false},
		{"ipv6 only", false, true},
		{"both", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			script := buildSwitchScript("WiFi", tc.wantIPv4, tc.wantIPv6)
			if !strings.Contains(script, "$want4="+psBool(tc.wantIPv4)) {
				t.Fatalf("script does not set the requested ipv4 state:\n%s", script)
			}
			if !strings.Contains(script, "$want6="+psBool(tc.wantIPv6)) {
				t.Fatalf("script does not set the requested ipv6 state:\n%s", script)
			}
		})
	}
}

func TestBuildSwitchScriptIsTransactional(t *testing.T) {
	script := buildSwitchScript("WiFi", false, true)

	for _, want := range []string{
		"Get-NetAdapter -Name $name",      // fail fast on a missing adapter
		"adapter-not-found",               // distinguishable error code
		"$was=[pscustomobject]",           // snapshot before changing anything
		"try {",                           // apply phase
		"} catch {",                       // rollback phase
		"$cur['ms_tcpip'] -ne $was.ipv4",  // restore ipv4
		"$cur['ms_tcpip6'] -ne $was.ipv6", // restore ipv6
		"$after=Read-Binding",             // verify after applying
		"verify-failed",
		"exit 1",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("switch script is missing %q", want)
		}
	}
}

func TestBuildSwitchScriptQuotesAdapterName(t *testing.T) {
	script := buildSwitchScript("Kid's Wi-Fi", true, true)
	if !strings.Contains(script, "$name='Kid''s Wi-Fi'") {
		t.Fatalf("adapter name was not single-quote escaped:\n%s", script)
	}
}

func TestParseSwitchResult(t *testing.T) {
	t.Run("success with leading warnings", func(t *testing.T) {
		raw := "WARNING: something\r\n" +
			`{"ok":true,"adapter":"WiFi","was":{"ipv4":true,"ipv6":true},"now":{"ipv4":false,"ipv6":true},"changed":["ipv4=False"],"rolledBack":false,"code":""}` + "\n"
		result := parseSwitchResult(raw)
		if result == nil {
			t.Fatal("parseSwitchResult() = nil")
		}
		if !result.OK || result.Adapter != "WiFi" {
			t.Fatalf("unexpected result: %+v", result)
		}
		if result.Was == nil || !result.Was.IPv4 || result.Now == nil || result.Now.IPv4 {
			t.Fatalf("unexpected state transition: was=%+v now=%+v", result.Was, result.Now)
		}
		if len(result.Changed) != 1 || result.Changed[0] != "ipv4=False" {
			t.Fatalf("unexpected changed list: %v", result.Changed)
		}
	})

	t.Run("failure with rollback", func(t *testing.T) {
		raw := `{"ok":false,"adapter":"WiFi","was":{"ipv4":true,"ipv6":false},"changed":["ipv6=True"],"rolledBack":true,"code":"verify-failed","error":"binding state did not change as requested"}` + "\n"
		result := parseSwitchResult(raw)
		if result == nil {
			t.Fatal("parseSwitchResult() = nil")
		}
		if result.OK || !result.RolledBack || result.Code != "verify-failed" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("unparseable output", func(t *testing.T) {
		if got := parseSwitchResult("Enable-NetAdapterBinding : Access is denied"); got != nil {
			t.Fatalf("parseSwitchResult() = %+v, want nil", got)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		if got := parseSwitchResult(""); got != nil {
			t.Fatalf("parseSwitchResult() = %+v, want nil", got)
		}
	})
}
