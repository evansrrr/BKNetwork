package handlers

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

func escapePowerShellSingleQuotedString(value string) string {
	value = strings.ReplaceAll(value, "`", "``")
	value = strings.ReplaceAll(value, "$", "`$")
	value = strings.ReplaceAll(value, "'", "''")
	return value
}

func joinPowerShellStringArray(values []string) string {
	if len(values) == 0 {
		return "@()"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("'%s'", escapePowerShellSingleQuotedString(value)))
	}
	return "@(" + strings.Join(parts, ",") + ")"
}

func normalizeDnsServerList(values []string) ([]string, error) {
	servers := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		candidate := trimmed
		if idx := strings.Index(candidate, "%"); idx >= 0 {
			candidate = candidate[:idx]
		}
		if net.ParseIP(candidate) == nil {
			return nil, fmt.Errorf("invalid dns server: %s", trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		servers = append(servers, trimmed)
	}
	return servers, nil
}

func splitDnsServersByFamily(values []string) ([]string, []string) {
	ipv4 := make([]string, 0, len(values))
	ipv6 := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, ":") {
			ipv6 = append(ipv6, trimmed)
			continue
		}
		ipv4 = append(ipv4, trimmed)
	}
	return ipv4, ipv6
}

func getAdapterDnsServers(ifName string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()

	// Try IPv4 DNS first
	entries, err := netshGetDNSServers(ctx, "ipv4")
	if err == nil {
		for _, entry := range entries {
			if strings.EqualFold(strings.TrimSpace(entry.Name), ifName) {
				return entry.Servers, nil
			}
		}
	}

	// Try IPv6 DNS
	entries, err = netshGetDNSServers(ctx, "ipv6")
	if err == nil {
		for _, entry := range entries {
			if strings.EqualFold(strings.TrimSpace(entry.Name), ifName) {
				return entry.Servers, nil
			}
		}
	}

	// Fallback to PowerShell if netsh fails
	psCmd := fmt.Sprintf("Get-DnsClientServerAddress | Where-Object { $_.InterfaceAlias -eq '%s' } | Select-Object InterfaceAlias, ServerAddresses | ConvertTo-Json -Compress", escapePowerShellSingleQuotedString(ifName))
	raw, psErr := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	if psErr != nil && strings.TrimSpace(raw) == "" {
		return nil, psErr
	}
	dnsInfos, decodeErr := decodeJSONList[dnsInfo](raw)
	if decodeErr != nil {
		return nil, decodeErr
	}
	servers := make([]string, 0)
	seen := make(map[string]struct{})
	for _, info := range dnsInfos {
		for _, server := range normalizeStringSlice(info.ServerAddresses) {
			if _, ok := seen[server]; ok {
				continue
			}
			seen[server] = struct{}{}
			servers = append(servers, server)
		}
	}
	return servers, nil
}

func setAdapterDnsServers(ifName string, servers []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutApply)
	defer cancel()

	psCmd := fmt.Sprintf("Set-DnsClientServerAddress -InterfaceAlias '%s' -ServerAddresses %s -Confirm:$false -ErrorAction Stop", escapePowerShellSingleQuotedString(ifName), joinPowerShellStringArray(servers))
	out, err := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	if err != nil {
		return out, err
	}
	// Wait for the OS to update the DNS configuration
	time.Sleep(300 * time.Millisecond)
	return out, nil
}

func applyDnsServers(ifName string, ipv4Servers, ipv6Servers *[]string) (string, error) {
	currentServers, err := getAdapterDnsServers(ifName)
	if err != nil {
		return "", err
	}
	currentIPv4, currentIPv6 := splitDnsServersByFamily(currentServers)

	nextIPv4 := currentIPv4
	if ipv4Servers != nil {
		nextIPv4, err = normalizeDnsServerList(*ipv4Servers)
		if err != nil {
			return "", err
		}
	}

	nextIPv6 := currentIPv6
	if ipv6Servers != nil {
		nextIPv6, err = normalizeDnsServerList(*ipv6Servers)
		if err != nil {
			return "", err
		}
	}

	combined := append(append(make([]string, 0, len(nextIPv4)+len(nextIPv6)), nextIPv4...), nextIPv6...)
	return setAdapterDnsServers(ifName, combined)
}
