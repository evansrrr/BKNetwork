package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// timeoutSwitch bounds the whole transactional switch. Each binding change can
// take a few seconds on Windows, and the script still has to roll back when one
// of them fails, so this is deliberately larger than the other timeouts.
const timeoutSwitch = 25 * time.Second

var (
	// errAdapterNotFound lets the HTTP layer answer 400 instead of 500.
	errAdapterNotFound = errors.New("adapter not found")
	// errUnknownMode is returned for modes other than ipv4/ipv6/both.
	errUnknownMode = errors.New("unknown mode")
)

func psBool(b bool) string {
	if b {
		return "$true"
	}
	return "$false"
}

// psSingleQuote quotes a value for a PowerShell single-quoted string. Only the
// quote character needs escaping there; $ and ` are literal.
func psSingleQuote(value string) string {
	return "'" + escapePowerShellSingleQuotedString(value) + "'"
}

// bindingState describes which IP stacks are bound to an adapter.
type bindingState struct {
	IPv4 bool `json:"ipv4"`
	IPv6 bool `json:"ipv6"`
}

// switchResult is the machine readable outcome of the switch script.
type switchResult struct {
	OK         bool          `json:"ok"`
	Code       string        `json:"code,omitempty"`
	Adapter    string        `json:"adapter,omitempty"`
	Was        *bindingState `json:"was,omitempty"`
	Now        *bindingState `json:"now,omitempty"`
	Changed    []string      `json:"changed,omitempty"`
	RolledBack bool          `json:"rolledBack"`
	Error      string        `json:"error,omitempty"`
}

// parseNetworkMode maps the API mode string to the desired binding state.
func parseNetworkMode(mode string) (wantIPv4, wantIPv6 bool, err error) {
	switch mode {
	case "ipv4":
		return true, false, nil
	case "ipv6":
		return false, true, nil
	case "both":
		return true, true, nil
	default:
		return false, false, fmt.Errorf("%w: %s", errUnknownMode, mode)
	}
}

// buildSwitchScript returns a PowerShell script that applies the requested
// binding state, verifies the result, and restores the previous state when any
// step fails. It prints exactly one JSON object (see switchResult); the exit
// code is non-zero when the switch failed.
func buildSwitchScript(ifName string, wantIPv4, wantIPv6 bool) string {
	name := psSingleQuote(ifName)
	var b strings.Builder

	b.WriteString("$ErrorActionPreference='Stop';")
	b.WriteString("$name=" + name + ";")
	b.WriteString("$want4=" + psBool(wantIPv4) + ";")
	b.WriteString("$want6=" + psBool(wantIPv6) + ";")
	b.WriteString("$code='apply-failed';")
	b.WriteString("$changed=@();")
	// $was has to exist in the catch block even when the failure happens before
	// the current state is read (for example a missing adapter).
	b.WriteString("$was=[pscustomobject]@{ ipv4=$null; ipv6=$null };")
	b.WriteString("function Set-Binding([string]$id,[bool]$enable) { " +
		"if ($enable) { Enable-NetAdapterBinding -Name $name -ComponentID $id -Confirm:$false -ErrorAction Stop } " +
		"else { Disable-NetAdapterBinding -Name $name -ComponentID $id -Confirm:$false -ErrorAction Stop } };")
	b.WriteString("function Read-Binding { $m=@{}; " +
		"Get-NetAdapterBinding -Name $name -ComponentID ms_tcpip,ms_tcpip6 -ErrorAction Stop | " +
		"ForEach-Object { $m[$_.ComponentID]=[bool]$_.Enabled }; return $m };")
	b.WriteString("try {")
	// Fail early with a distinguishable code when the adapter does not exist:
	// without this the script would try to enable a binding on nothing and
	// return an opaque COM error.
	b.WriteString(" if (-not (Get-NetAdapter -Name $name -ErrorAction SilentlyContinue)) { " +
		"$code='adapter-not-found'; throw ('adapter not found: ' + $name) };")
	b.WriteString(" $state=Read-Binding;")
	b.WriteString(" $was=[pscustomobject]@{ ipv4=$state['ms_tcpip']; ipv6=$state['ms_tcpip6'] };")
	b.WriteString(" if ($was.ipv4 -ne $want4) { Set-Binding 'ms_tcpip' $want4; $changed += ('ipv4=' + $want4) };")
	b.WriteString(" if ($was.ipv6 -ne $want6) { Set-Binding 'ms_tcpip6' $want6; $changed += ('ipv6=' + $want6) };")
	b.WriteString(" $after=Read-Binding;")
	b.WriteString(" if ($after['ms_tcpip'] -ne $want4 -or $after['ms_tcpip6'] -ne $want6) { " +
		"$code='verify-failed'; throw ('binding state did not change as requested (ipv4=' + $after['ms_tcpip'] + ', ipv6=' + $after['ms_tcpip6'] + ')') };")
	b.WriteString(" [pscustomobject]@{ ok=$true; adapter=$name; was=$was; " +
		"now=[pscustomobject]@{ ipv4=$after['ms_tcpip']; ipv6=$after['ms_tcpip6'] }; " +
		"changed=@($changed); rolledBack=$false; code='' } | ConvertTo-Json -Compress -Depth 4;")
	b.WriteString("} catch {")
	b.WriteString(" $message=$_.Exception.Message; $rolled=$false;")
	b.WriteString(" try { $cur=Read-Binding; " +
		"if ($null -ne $was.ipv4 -and $cur['ms_tcpip'] -ne $was.ipv4) { Set-Binding 'ms_tcpip' $was.ipv4 }; " +
		"if ($null -ne $was.ipv6 -and $cur['ms_tcpip6'] -ne $was.ipv6) { Set-Binding 'ms_tcpip6' $was.ipv6 }; " +
		"$rolled=$true } catch { $rolled=$false };")
	b.WriteString(" [pscustomobject]@{ ok=$false; adapter=$name; was=$was; now=$null; " +
		"changed=@($changed); rolledBack=$rolled; code=$code; error=$message } | ConvertTo-Json -Compress -Depth 4;")
	b.WriteString(" exit 1;")
	b.WriteString("}")

	return b.String()
}

// parseSwitchResult extracts the JSON object printed by the switch script. The
// combined output may contain PowerShell warnings first, so the last line that
// looks like a result object wins.
func parseSwitchResult(raw string) *switchResult {
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "\"ok\"") {
			continue
		}
		var result switchResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			continue
		}
		return &result
	}
	return nil
}

// applyNetworkMode switches the adapter to the requested stack. It returns the
// structured result (may be nil when the script produced no parseable output),
// the raw output, and an error describing whether the change was rolled back.
func applyNetworkMode(ifName, mode string) (*switchResult, string, error) {
	wantIPv4, wantIPv6, err := parseNetworkMode(mode)
	if err != nil {
		return nil, "", err
	}

	if runtime.GOOS == "darwin" {
		out, err := applyDarwinNetworkMode(ifName, mode)
		if err != nil {
			return nil, out, err
		}
		return &switchResult{
			OK:      true,
			Adapter: ifName,
			Was:     &bindingState{IPv4: wantIPv4, IPv6: wantIPv6},
			Now:     &bindingState{IPv4: wantIPv4, IPv6: wantIPv6},
		}, out, nil
	}

	script := buildSwitchScript(ifName, wantIPv4, wantIPv6)
	// Deliberately not derived from the request context: the script has to be
	// allowed to finish its rollback even if the client is already gone.
	ctx, cancel := context.WithTimeout(context.Background(), timeoutSwitch)
	defer cancel()

	out, runErr := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	result := parseSwitchResult(out)
	if runErr == nil {
		if result == nil {
			return nil, out, errors.New("switch script returned no result")
		}
		return result, out, nil
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, out, fmt.Errorf("switch timed out after %s, adapter state was not verified: %w", timeoutSwitch, ctx.Err())
	}
	if result != nil {
		switch {
		case result.Code == "adapter-not-found":
			return result, out, fmt.Errorf("%w: %s", errAdapterNotFound, ifName)
		case result.Error != "" && len(result.Changed) == 0:
			// Nothing was applied, so there is nothing to restore and the
			// adapter state cannot be inconsistent.
			return result, out, fmt.Errorf("%s (no changes were applied)", result.Error)
		case result.Error != "" && result.RolledBack:
			return result, out, fmt.Errorf("%s (previous configuration restored)", result.Error)
		case result.Error != "":
			return result, out, fmt.Errorf("%s (configuration may be inconsistent)", result.Error)
		}
	}
	return result, out, fmt.Errorf("switch failed: %w: %s", runErr, strings.TrimSpace(out))
}
