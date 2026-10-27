package handlers

import (
	"context"
	"net"
	"regexp"
	"strconv"
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

// parseNetshInterfaceConnected parses output from
// "netsh interface ipv4/ipv6 show interface" (both families share the layout).
// Returns adapter names and their connected state.
func parseNetshInterfaceConnected(raw string) map[string]bool {
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

// parseNetshInterfaceIndex parses output from
// "netsh interface ipv4/ipv6 show interface" and maps interface index to name.
func parseNetshInterfaceIndex(raw string) map[string]string {
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

// isWarpAdapterName reports whether the adapter name refers to the Cloudflare WARP tunnel.
func isWarpAdapterName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(n, "warp") || strings.Contains(n, "cloudflare")
}

// defaultRouteCandidate holds one default-route entry parsed from the route table.
type defaultRouteCandidate struct {
	name   string
	metric int
	isWarp bool
}

// pickDefaultRoute returns the best interface name among default-route candidates.
// Preference order: non-WARP over WARP, then lower metric (higher priority).
func pickDefaultRoute(candidates []defaultRouteCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.isWarp != best.isWarp {
			if !c.isWarp {
				best = c
			}
			continue
		}
		if c.metric < best.metric {
			best = c
		}
	}
	return best.name
}

// parseNetshIPv4DefaultRouteWithIdx parses the IPv4 route table and returns the
// interface name that should be treated as the default (physical) route.
// When WARP is active it also installs a default route; prefer the non-WARP
// interface so downstream logic does not mistakenly target the tunnel adapter.
func parseNetshIPv4DefaultRouteWithIdx(raw string, ifaceMap map[string]string) string {
	var candidates []defaultRouteCandidate
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
		for i, f := range fields {
			if f != "0.0.0.0/0" {
				continue
			}
			// Layout: ... Metric Prefix Idx Gateway Interface ...
			if i < 1 || i+1 >= len(fields) {
				continue
			}
			metric, _ := strconv.Atoi(fields[i-1])
			idx := fields[i+1]
			name, ok := ifaceMap[idx]
			if !ok {
				continue
			}
			candidates = append(candidates, defaultRouteCandidate{
				name:   name,
				metric: metric,
				isWarp: isWarpAdapterName(name),
			})
		}
	}
	return pickDefaultRoute(candidates)
}

// parseNetshIPv6DefaultRouteWithIdx parses the IPv6 route table and returns the
// interface name that should be treated as the default (physical) route.
func parseNetshIPv6DefaultRouteWithIdx(raw string, ifaceMap map[string]string) string {
	var candidates []defaultRouteCandidate
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
		for i, f := range fields {
			if f != "::/0" {
				continue
			}
			// Layout: ... Metric Prefix Idx Gateway Interface ...
			if i < 1 || i+1 >= len(fields) {
				continue
			}
			metric, _ := strconv.Atoi(fields[i-1])
			idx := fields[i+1]
			name, ok := ifaceMap[idx]
			if !ok {
				continue
			}
			candidates = append(candidates, defaultRouteCandidate{
				name:   name,
				metric: metric,
				isWarp: isWarpAdapterName(name),
			})
		}
	}
	return pickDefaultRoute(candidates)
}

// parseNetshDNSServers parses output from "netsh interface ipv4/ipv6 show dnsservers".
// Returns a list of netshDNSEntry with DNS server information.
//
// netsh prints the first server on the key line and any additional servers on
// indented continuation lines:
//
//	Statically Configured DNS Servers:    8.8.8.8
//	                                      1.1.1.1
//
// Indentation therefore has to be checked against the raw line: after
// TrimSpace the leading whitespace is gone and the continuation branch can
// never match.
func parseNetshDNSServers(raw string) []netshDNSEntry {
	var entries []netshDNSEntry
	var current *netshDNSEntry

	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(rawLine, "\ufeff"))

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
			if value := strings.TrimSpace(line[idx+1:]); isDNSServerToken(value) {
				current.Servers = append(current.Servers, value)
			}
			continue
		}

		// Continuation lines are indented and hold a bare address. The address
		// check is what separates them from labels such as
		// "Register with which suffix:" or from IPv6 addresses (which contain
		// colons themselves).
		if len(rawLine) > 0 && (rawLine[0] == ' ' || rawLine[0] == '\t') && isDNSServerToken(line) {
			current.Servers = append(current.Servers, line)
		}
	}
	return entries
}

// isDNSServerToken reports whether value looks like a DNS server address.
// It rejects empty values and the "None" placeholder netsh prints when a
// family has no servers configured.
func isDNSServerToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") {
		return false
	}
	if idx := strings.Index(value, "%"); idx >= 0 {
		value = value[:idx]
	}
	value = strings.Trim(value, "[]")
	return net.ParseIP(value) != nil
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

// netshInterfaceState is the parsed result of "netsh interface ipvX show
// interface": adapter names mapped to their connected state, plus the interface
// index mapping needed to interpret the route table.
type netshInterfaceState struct {
	Connected map[string]bool
	Index     map[string]string
}

// netshGetInterfaces runs "netsh interface ipvX show interface" once and returns
// both the connected state and the index mapping, so callers do not have to run
// the same command again for the route table.
func netshGetInterfaces(ctx context.Context, family string) (netshInterfaceState, error) {
	raw, err := execWithTimeout(ctx, "netsh", "interface", family, "show", "interface")
	if err != nil && strings.TrimSpace(raw) == "" {
		return netshInterfaceState{}, err
	}
	return netshInterfaceState{
		Connected: parseNetshInterfaceConnected(raw),
		Index:     parseNetshInterfaceIndex(raw),
	}, nil
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
func netshGetIPv4DefaultRoute(ctx context.Context, ifaceMap map[string]string) (string, error) {
	routeRaw, err := execWithTimeout(ctx, "netsh", "interface", "ipv4", "show", "route")
	if err != nil && strings.TrimSpace(routeRaw) == "" {
		return "", err
	}
	return parseNetshIPv4DefaultRouteWithIdx(routeRaw, ifaceMap), nil
}

// netshGetIPv6DefaultRoute gets the interface name for the IPv6 default route.
func netshGetIPv6DefaultRoute(ctx context.Context, ifaceMap map[string]string) (string, error) {
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
