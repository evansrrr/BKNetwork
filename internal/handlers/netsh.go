package handlers

import (
	"context"
	"regexp"
	"strings"
)

// netshAdapterBinding represents the IPv4/IPv6 binding state of a network adapter.
type netshAdapterBinding struct {
	Name        string
	IPv4Enabled bool
	IPv6Enabled bool
}

// netshIPConfig represents IP configuration for a network adapter.
type netshIPConfig struct {
	Name        string
	IPv4Gateway string
	IPv6Gateway string
}

// netshDNSEntry represents DNS servers for a network adapter.
type netshDNSEntry struct {
	Name    string
	Servers []string
}

// parseNetshShowInterface parses output from "netsh interface show interface".
// Returns a list of adapter names and their connected state.
func parseNetshShowInterface(raw string) map[string]bool {
	result := make(map[string]bool)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Admin") || strings.HasPrefix(line, "---") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		adminState := fields[0]
		state := fields[1]
		nameIdx := -1
		for i, f := range fields {
			if f == "Dedicated" || f == "Loopback" || f == "Internal" || f == "External" {
				nameIdx = i + 1
				break
			}
		}
		if nameIdx < 0 || nameIdx >= len(fields) {
			continue
		}
		name := strings.Join(fields[nameIdx:], " ")
		connected := strings.EqualFold(state, "connected") && strings.EqualFold(adminState, "enabled")
		result[name] = connected
	}
	return result
}

// parseNetshIPv4Interface parses output from "netsh interface ipv4 show interface".
// Returns adapter names and their connected state.
func parseNetshIPv4Interface(raw string) map[string]bool {
	result := make(map[string]bool)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Idx") || strings.HasPrefix(line, "---") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		state := fields[3]
		name := strings.Join(fields[4:], " ")
		result[name] = strings.EqualFold(state, "connected")
	}
	return result
}

// parseNetshIPv4InterfaceWithIdx parses output from "netsh interface ipv4 show interface".
// Returns a map of interface index to name.
func parseNetshIPv4InterfaceWithIdx(raw string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Idx") || strings.HasPrefix(line, "---") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		idx := fields[0]
		name := strings.Join(fields[4:], " ")
		result[idx] = name
	}
	return result
}

// parseNetshIPv6Interface parses output from "netsh interface ipv6 show interface".
// Returns adapter names and their connected state.
func parseNetshIPv6Interface(raw string) map[string]bool {
	return parseNetshIPv4Interface(raw) // Same format
}

// parseNetshIPv6InterfaceWithIdx parses output from "netsh interface ipv6 show interface".
// Returns a map of interface index to name.
func parseNetshIPv6InterfaceWithIdx(raw string) map[string]string {
	return parseNetshIPv4InterfaceWithIdx(raw) // Same format
}

// parseNetshIPv4Config parses output from "netsh interface ipv4 show config".
// Returns a list of netshIPConfig with gateway information.
func parseNetshIPv4Config(raw string) []netshIPConfig {
	var configs []netshIPConfig
	var current *netshIPConfig

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect interface header: Configuration for interface "Name"
		if strings.HasPrefix(line, "Configuration for interface") {
			name := extractQuotedString(line)
			if name != "" {
				configs = append(configs, netshIPConfig{Name: name})
				current = &configs[len(configs)-1]
			}
			continue
		}

		if current == nil {
			continue
		}

		// Extract Default Gateway
		if idx := strings.Index(line, "Default Gateway:"); idx >= 0 {
			gw := strings.TrimSpace(line[idx+len("Default Gateway:"):])
			if gw != "" && !strings.EqualFold(gw, "None") {
				current.IPv4Gateway = gw
			}
		}
	}
	return configs
}

// parseNetshIPv4DefaultRouteWithIdx parses the route table and returns the interface name for the default route.
func parseNetshIPv4DefaultRouteWithIdx(raw string, ifaceMap map[string]string) string {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Publish") || strings.HasPrefix(line, "---") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Find the prefix field (contains "0.0.0.0/0" for default route)
		for i, f := range fields {
			if f == "0.0.0.0/0" {
				// Idx is at position 4 (after Publish, Type, Met, Prefix)
				if i+1 < len(fields) {
					idx := fields[i+1]
					if name, ok := ifaceMap[idx]; ok {
						return name
					}
				}
			}
		}
	}
	return ""
}

// parseNetshIPv6DefaultRouteWithIdx parses the route table and returns the interface name for the default route.
func parseNetshIPv6DefaultRouteWithIdx(raw string, ifaceMap map[string]string) string {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Publish") || strings.HasPrefix(line, "---") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Find the prefix field (contains "::/0" for default route)
		for i, f := range fields {
			if f == "::/0" {
				// Idx is at position 4 (after Publish, Type, Met, Prefix)
				if i+1 < len(fields) {
					idx := fields[i+1]
					if name, ok := ifaceMap[idx]; ok {
						return name
					}
				}
			}
		}
	}
	return ""
}

// parseNetshDNSServers parses output from "netsh interface ipv4/ipv6 show dnsservers".
// Returns a list of netshDNSEntry with DNS server information.
func parseNetshDNSServers(raw string) []netshDNSEntry {
	var entries []netshDNSEntry
	var current *netshDNSEntry

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect interface header
		if strings.HasPrefix(line, "Configuration for interface") {
			name := extractQuotedString(line)
			if name != "" {
				entries = append(entries, netshDNSEntry{Name: name})
				current = &entries[len(entries)-1]
			}
			continue
		}

		if current == nil {
			continue
		}

		// Extract DNS servers - two patterns:
		// "DNS servers configured through DHCP:  202.204.52.94"
		// "Statically Configured DNS Servers:    8.8.8.8"
		if (strings.Contains(line, "DNS servers") || strings.Contains(line, "DNS Servers")) && strings.Contains(line, ":") {
			idx := strings.Index(line, ":")
			value := strings.TrimSpace(line[idx+1:])
			if value != "" && !strings.EqualFold(value, "None") {
				current.Servers = append(current.Servers, value)
			}
			continue
		}

		// Continuation lines (indented, no key) are additional DNS servers
		if len(line) > 0 && line[0] == ' ' && current.Servers != nil {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.Contains(trimmed, ":") {
				current.Servers = append(current.Servers, trimmed)
			}
		}
	}
	return entries
}

// extractQuotedString extracts a string from double quotes in a line.
func extractQuotedString(line string) string {
	start := strings.Index(line, "\"")
	if start < 0 {
		return ""
	}
	end := strings.Index(line[start+1:], "\"")
	if end < 0 {
		return ""
	}
	return line[start+1 : start+1+end]
}

// netshGetIPv4Binding gets the IPv4/IPv6 binding state for all adapters.
func netshGetIPv4Binding(ctx context.Context) (map[string]bool, error) {
	raw, err := execWithTimeout(ctx, "netsh", "interface", "ipv4", "show", "interface")
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	return parseNetshIPv4Interface(raw), nil
}

// netshGetIPv6Binding gets the IPv6 binding state for all adapters.
func netshGetIPv6Binding(ctx context.Context) (map[string]bool, error) {
	raw, err := execWithTimeout(ctx, "netsh", "interface", "ipv6", "show", "interface")
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	return parseNetshIPv6Interface(raw), nil
}

// netshGetIPv4Config gets IPv4 configuration including gateway.
func netshGetIPv4Config(ctx context.Context) ([]netshIPConfig, error) {
	raw, err := execWithTimeout(ctx, "netsh", "interface", "ipv4", "show", "config")
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	return parseNetshIPv4Config(raw), nil
}

// netshGetIPv4DefaultRoute gets the interface name for the IPv4 default route.
func netshGetIPv4DefaultRoute(ctx context.Context) (string, error) {
	// Get interface list first to map index to name
	ifaceRaw, err := execWithTimeout(ctx, "netsh", "interface", "ipv4", "show", "interface")
	if err != nil && strings.TrimSpace(ifaceRaw) == "" {
		return "", err
	}
	ifaceMap := parseNetshIPv4InterfaceWithIdx(ifaceRaw)

	// Get route table
	routeRaw, err := execWithTimeout(ctx, "netsh", "interface", "ipv4", "show", "route")
	if err != nil && strings.TrimSpace(routeRaw) == "" {
		return "", err
	}
	return parseNetshIPv4DefaultRouteWithIdx(routeRaw, ifaceMap), nil
}

// netshGetIPv6DefaultRoute gets the interface name for the IPv6 default route.
func netshGetIPv6DefaultRoute(ctx context.Context) (string, error) {
	// Get interface list first to map index to name
	ifaceRaw, err := execWithTimeout(ctx, "netsh", "interface", "ipv6", "show", "interface")
	if err != nil && strings.TrimSpace(ifaceRaw) == "" {
		return "", err
	}
	ifaceMap := parseNetshIPv6InterfaceWithIdx(ifaceRaw)

	// Get route table
	routeRaw, err := execWithTimeout(ctx, "netsh", "interface", "ipv6", "show", "route")
	if err != nil && strings.TrimSpace(routeRaw) == "" {
		return "", err
	}
	return parseNetshIPv6DefaultRouteWithIdx(routeRaw, ifaceMap), nil
}

// netshGetDNSServers gets DNS servers for all adapters.
func netshGetDNSServers(ctx context.Context, family string) ([]netshDNSEntry, error) {
	raw, err := execWithTimeout(ctx, "netsh", "interface", family, "show", "dnsservers")
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	return parseNetshDNSServers(raw), nil
}

// escapePowerShellSingleQuotedString is also used for PowerShell commands in other files.
// This regex is used for parsing netsh output.
var netshWhitespaceRegex = regexp.MustCompile(`\s+`)
