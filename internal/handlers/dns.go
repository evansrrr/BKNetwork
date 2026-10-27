package handlers

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"
)

// escapePowerShellSingleQuotedString escapes a value that is embedded in a
// single-quoted string. Inside single quotes only the quote character is
// special: backticks and $ are literal, so prefixing them with a backtick (as
// this used to do) changed the value instead of escaping it, and an adapter
// named e.g. "IPv6$" could no longer be found.
func escapePowerShellSingleQuotedString(value string) string {
	return strings.ReplaceAll(value, "'", "''")
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

// getAdapterDnsServers returns the DNS servers configured on the adapter,
// split by address family. Both families are always queried: callers merge the
// returned lists back into a single Set-DnsClientServerAddress call, so
// stopping at the first matching family would silently drop the other one.
func getAdapterDnsServers(ifName string) ([]string, []string, error) {
	if runtime.GOOS == "darwin" {
		servers, err := getDarwinAdapterDnsServers(ifName)
		if err != nil {
			return nil, nil, err
		}
		ipv4, ipv6 := splitDnsServersByFamily(servers)
		return ipv4, ipv6, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutMedium)
	defer cancel()

	entriesIPv4, errIPv4 := netshGetDNSServers(ctx, "ipv4")
	entriesIPv6, errIPv6 := netshGetDNSServers(ctx, "ipv6")

	var (
		ipv4     []string
		ipv6     []string
		found    bool
		firstErr = errIPv4
	)
	if firstErr == nil {
		firstErr = errIPv6
	}
	for _, entry := range entriesIPv4 {
		if strings.EqualFold(strings.TrimSpace(entry.Name), ifName) {
			ipv4, _ = splitDnsServersByFamily(entry.Servers)
			found = true
			break
		}
	}
	for _, entry := range entriesIPv6 {
		if strings.EqualFold(strings.TrimSpace(entry.Name), ifName) {
			_, ipv6 = splitDnsServersByFamily(entry.Servers)
			found = true
			break
		}
	}
	if found || firstErr == nil {
		return ipv4, ipv6, nil
	}

	// Both netsh queries failed; fall back to PowerShell.
	return getAdapterDnsServersViaPowerShell(ifName)
}

func getAdapterDnsServersViaPowerShell(ifName string) ([]string, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutMedium)
	defer cancel()

	psCmd := fmt.Sprintf("Get-DnsClientServerAddress | Where-Object { $_.InterfaceAlias -eq '%s' } | Select-Object InterfaceAlias, ServerAddresses | ConvertTo-Json -Compress", escapePowerShellSingleQuotedString(ifName))
	raw, psErr := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	if psErr != nil && strings.TrimSpace(raw) == "" {
		return nil, nil, psErr
	}
	dnsInfos, decodeErr := decodeJSONList[dnsInfo](raw)
	if decodeErr != nil {
		return nil, nil, decodeErr
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
	ipv4, ipv6 := splitDnsServersByFamily(servers)
	return ipv4, ipv6, nil
}

// planDNSServers merges the requested changes with the current configuration.
// Families that were not part of the request keep their current servers; the
// merged list is what gets written back to the adapter.
func planDNSServers(currentIPv4, currentIPv6 []string, ipv4Servers, ipv6Servers *[]string) ([]string, error) {
	nextIPv4 := append([]string(nil), currentIPv4...)
	if ipv4Servers != nil {
		normalized, err := normalizeDnsServerList(*ipv4Servers)
		if err != nil {
			return nil, err
		}
		nextIPv4 = normalized
	}

	nextIPv6 := append([]string(nil), currentIPv6...)
	if ipv6Servers != nil {
		normalized, err := normalizeDnsServerList(*ipv6Servers)
		if err != nil {
			return nil, err
		}
		nextIPv6 = normalized
	}

	return append(nextIPv4, nextIPv6...), nil
}

func setAdapterDnsServers(ifName string, servers []string) (string, error) {
	if runtime.GOOS == "darwin" {
		return setDarwinAdapterDnsServers(ifName, servers)
	}
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
	currentIPv4, currentIPv6, err := getAdapterDnsServers(ifName)
	if err != nil {
		return "", err
	}

	combined, err := planDNSServers(currentIPv4, currentIPv6, ipv4Servers, ipv6Servers)
	if err != nil {
		return "", err
	}
	return setAdapterDnsServers(ifName, combined)
}
