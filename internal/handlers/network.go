package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func psBool(b bool) string {
	if b {
		return "$true"
	}
	return "$false"
}

func applyNetworkMode(ifName, mode string) (string, error) {
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
	return execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script.String())
}

func getIPv6AdminState(ifName string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()

	psCmd := fmt.Sprintf("(Get-NetAdapterBinding -Name '%s' -ComponentID ms_tcpip6 -ErrorAction SilentlyContinue).Enabled -eq $true", ifName)
	out, err := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	if err == nil {
		s := strings.TrimSpace(strings.ToLower(out))
		switch {
		case strings.Contains(s, "true") || strings.Contains(s, "1"):
			return true, nil
		case strings.Contains(s, "false") || strings.Contains(s, "0"):
			return false, nil
		}
	}

	out, err = execWithTimeout(ctx, "netsh", "interface", "ipv6", "show", "interface", ifName)
	if err != nil && out == "" {
		return false, err
	}
	text := strings.ToLower(out)
	if strings.Contains(text, "disabled") {
		return false, nil
	}
	if strings.Contains(text, "enabled") {
		return true, nil
	}
	if strings.Contains(text, "admin") && strings.Contains(text, "state") {
		if strings.Contains(text, "0") {
			return false, nil
		}
	}
	return false, errors.New("unable to determine ipv6 admin state")
}
