//go:build windows

package handlers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// switchHarnessFunctions shadows the NetAdapter cmdlets with an in-memory model,
// so the tests exercise the real PowerShell control flow (snapshot, apply,
// verify, rollback) without touching a physical adapter. Every call is appended
// to a log file, which is what the assertions look at: the generated script
// exits with "exit 1" on failure, so nothing can be printed afterwards.
const switchHarnessFunctions = `
function Get-NetAdapter { param([string]$Name,[object]$ErrorAction) [pscustomobject]@{ Name=$Name } }
function Get-NetAdapterBinding { param([string]$Name,[string[]]$ComponentID,[object]$ErrorAction)
  foreach ($id in $ComponentID) { [pscustomobject]@{ ComponentID=$id; Enabled=[bool]$global:state[$id] } } }
function Enable-NetAdapterBinding { param([string]$Name,[string]$ComponentID,[switch]$Confirm,[object]$ErrorAction)
  Add-Content -Path $global:logFile -Value ('enable ' + $ComponentID)
  if ($ComponentID -eq $script:failComponent) { throw ('injected failure enabling ' + $ComponentID) }
  $global:state[$ComponentID]=$true }
function Disable-NetAdapterBinding { param([string]$Name,[string]$ComponentID,[switch]$Confirm,[object]$ErrorAction)
  Add-Content -Path $global:logFile -Value ('disable ' + $ComponentID)
  if ($ComponentID -eq $script:failComponent) { throw ('injected failure disabling ' + $ComponentID) }
  $global:state[$ComponentID]=$false }
`

func runSwitchHarness(t *testing.T, script string, initialIPv4, initialIPv6 bool, failComponent string) (result *switchResult, calls []string, err error) {
	t.Helper()
	ps := func(b bool) string {
		if b {
			return "$true"
		}
		return "$false"
	}

	callLog := filepath.Join(t.TempDir(), "calls.log")

	full := "$global:state=@{};" +
		"$global:state['ms_tcpip']=" + ps(initialIPv4) + ";" +
		"$global:state['ms_tcpip6']=" + ps(initialIPv6) + ";" +
		"$global:logFile='" + callLog + "';" +
		"$script:failComponent='" + failComponent + "';" +
		switchHarnessFunctions +
		script

	out, runErr := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", full).CombinedOutput()

	if data, readErr := os.ReadFile(callLog); readErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				calls = append(calls, trimmed)
			}
		}
	}
	return parseSwitchResult(string(out)), calls, runErr
}

func TestSwitchScriptSimulatedSuccess(t *testing.T) {
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("powershell not available")
	}
	result, calls, err := runSwitchHarness(t, buildSwitchScript("Fake Adapter", false, true), true, true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v (calls=%v)", err, calls)
	}
	if result == nil || !result.OK {
		t.Fatalf("result = %+v, want ok", result)
	}
	if result.Was == nil || !result.Was.IPv4 || result.Now == nil || result.Now.IPv4 || !result.Now.IPv6 {
		t.Fatalf("unexpected state transition: was=%+v now=%+v", result.Was, result.Now)
	}
	if len(result.Changed) != 1 || result.Changed[0] != "ipv4=False" {
		t.Fatalf("changed = %v, want [ipv4=False]", result.Changed)
	}
	// IPv6 was already enabled, so only IPv4 must be touched.
	if len(calls) != 1 || calls[0] != "disable ms_tcpip" {
		t.Fatalf("binding calls = %v, want [disable ms_tcpip]", calls)
	}
}

func TestSwitchScriptSimulatedRollback(t *testing.T) {
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("powershell not available")
	}
	// Mode "ipv4" enables IPv4 first, then disables IPv6. Injecting the failure
	// into the second step means the first change has to be undone.
	result, calls, err := runSwitchHarness(t, buildSwitchScript("Fake Adapter", true, false), false, true, "ms_tcpip6")
	if err == nil {
		t.Fatalf("expected the script to fail, calls=%v", calls)
	}
	if result == nil {
		t.Fatalf("no result parsed from output (calls=%v)", calls)
	}
	if result.OK {
		t.Fatalf("result = %+v, want ok=false", result)
	}
	if !result.RolledBack {
		t.Fatal("result.RolledBack = false, want true")
	}
	if result.Code != "apply-failed" {
		t.Fatalf("result.Code = %q, want apply-failed", result.Code)
	}
	if !strings.Contains(result.Error, "injected failure") {
		t.Fatalf("result.Error = %q, want the injected failure", result.Error)
	}
	want := []string{"enable ms_tcpip", "disable ms_tcpip6", "disable ms_tcpip"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("binding calls = %v, want %v (last call proves the rollback)", calls, want)
	}
}

func TestSwitchScriptSimulatedAdapterMissing(t *testing.T) {
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("powershell not available")
	}
	// Force the "adapter not found" branch without depending on the machine.
	script := strings.Replace(
		buildSwitchScript("Missing", true, true),
		"if (-not (Get-NetAdapter -Name $name -ErrorAction SilentlyContinue))",
		"if ($true)",
		1,
	)
	result, calls, err := runSwitchHarness(t, script, true, true, "")
	if err == nil {
		t.Fatal("expected the script to fail")
	}
	if result == nil || result.Code != "adapter-not-found" {
		t.Fatalf("result = %+v, want code adapter-not-found", result)
	}
	if len(calls) != 0 {
		t.Fatalf("binding calls = %v, want none", calls)
	}
}
