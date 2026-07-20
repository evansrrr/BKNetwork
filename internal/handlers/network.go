package handlers

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

func psBool(b bool) string {
	if b {
		return "$true"
	}
	return "$false"
}

func applyNetworkMode(ifName, mode string) (string, error) {
	if runtime.GOOS == "darwin" {
		return applyDarwinNetworkMode(ifName, mode)
	}
	var wantIPv4, wantIPv6 bool
	switch mode {
	case "ipv4":
		wantIPv4, wantIPv6 = true, false
	case "ipv6":
		wantIPv4, wantIPv6 = false, true
	case "both":
		wantIPv4, wantIPv6 = true, true
	default:
		return "", fmt.Errorf("unknown mode: %s", mode)
	}

	escaped := escapePowerShellSingleQuotedString(ifName)
	v4, v6 := psBool(wantIPv4), psBool(wantIPv6)

	var script strings.Builder
	script.WriteString("$ErrorActionPreference='Stop';")
	script.WriteString(fmt.Sprintf("$b=Get-NetAdapterBinding -Name '%s' -ComponentID ms_tcpip,ms_tcpip6 -ErrorAction SilentlyContinue", escaped))
	script.WriteString(";$cur=@{};$b|ForEach-Object{$cur[$_.ComponentID]=$_.Enabled}")
	script.WriteString(fmt.Sprintf(";$r='was:'+($cur['ms_tcpip'])+','+($cur['ms_tcpip6'])"))
	script.WriteString(fmt.Sprintf(";if($cur['ms_tcpip'] -ne $true -and %s -eq $true){Enable-NetAdapterBinding -Name '%s' -ComponentID ms_tcpip -Confirm:$false;$r+='|en4'}", v4, escaped))
	script.WriteString(fmt.Sprintf(";if($cur['ms_tcpip'] -eq $true -and %s -eq $false){Disable-NetAdapterBinding -Name '%s' -ComponentID ms_tcpip -Confirm:$false;$r+='|dis4'}", v4, escaped))
	script.WriteString(fmt.Sprintf(";if($cur['ms_tcpip6'] -ne $true -and %s -eq $true){Enable-NetAdapterBinding -Name '%s' -ComponentID ms_tcpip6 -Confirm:$false;$r+='|en6'}", v6, escaped))
	script.WriteString(fmt.Sprintf(";if($cur['ms_tcpip6'] -eq $true -and %s -eq $false){Disable-NetAdapterBinding -Name '%s' -ComponentID ms_tcpip6 -Confirm:$false;$r+='|dis6'}", v6, escaped))
	script.WriteString(";$r")

	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()
	out, err := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script.String())
	if err != nil {
		return out, err
	}
	// Wait for the OS to update the network adapter state
	time.Sleep(1 * time.Second)
	return out, nil
}

func getIPv6AdminState(ifName string) (bool, error) {
	if runtime.GOOS == "darwin" {
		return getDarwinIPv6State(ifName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()

	raw, err := execWithTimeout(ctx, "netsh", "interface", "ipv6", "show", "interface")
	if err != nil && raw == "" {
		return false, err
	}
	// Parse netsh output to find the adapter and its state
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 5 {
			continue
		}
		name := strings.Join(fields[4:], " ")
		if !strings.EqualFold(name, ifName) {
			continue
		}
		state := strings.ToLower(fields[3])
		return state == "connected", nil
	}
	return false, errors.New("adapter not found")
}
