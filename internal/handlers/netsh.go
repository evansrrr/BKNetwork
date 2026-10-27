package handlers

import (
	"context"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// netshIPConfig, parseNetshIPv4Config and netshGetIPv4Config were removed with
// M1: the gateway is read from the default route row instead. That row carries
// the gateway address itself, so the parser no longer depends on the localized
// "Default Gateway:" label of "netsh interface ipv4 show config", and one
// external process per snapshot disappears with it.

// netshDNSEntry represents DNS servers for a network adapter.
type netshDNSEntry struct {
	Name    string
	Servers []string
}

// netshConnectedWords and netshDisconnectedWords translate the State column of
// "netsh interface ipvX show interface". netsh prints it in the system display
// language: on a Chinese Windows the column reads 已连接/已断开连接, so
// comparing against "connected" alone reported every adapter as disconnected,
// which in turn disabled the DNS editor, freeFlow and the DNS64 decision.
//
// The lists cover the languages the app is realistically used in; anything else
// is reported as unknown so the snapshot can fall back to the value
// PowerShell reported instead of silently claiming "disconnected".
var netshConnectedWords = []string{
	"connected",
	"已连接",
	"已連線",
	"已连线",
	"接続済み",
	"接続",
	"verbunden",
	"connecté",
	"conectado",
	"подключено",
	"연결됨",
	"bağlı",
	"połączono",
	"verbonden",
	"connesso",
	"collegato",
}

var netshDisconnectedWords = []string{
	"disconnected",
	"已断开连接",
	"已中斷連線",
	"已断开",
	"断开连接",
	"未接続",
	"切断",
	"nicht verbunden",
	"getrennt",
	"déconnecté",
	"non connecté",
	"desconectado",
	"отключено",
	"연결 끊김",
	"bağlı değil",
	"rozłączono",
	"niet verbonden",
	"non connesso",
	"disconnesso",
}

// netshStateConnected reports whether the state word means "connected". The
// second result is false when the word is not known, which lets callers fall
// back instead of guessing.
func netshStateConnected(word string) (connected bool, known bool) {
	normalized := strings.ToLower(strings.TrimSpace(word))
	if normalized == "" {
		return false, false
	}
	for _, candidate := range netshDisconnectedWords {
		if normalized == candidate {
			return false, true
		}
	}
	for _, candidate := range netshConnectedWords {
		if normalized == candidate {
			return true, true
		}
	}
	// Variants such as "disconnected (site-local)" or "not connected".
	if strings.Contains(normalized, "disconnect") || strings.Contains(normalized, "not connect") {
		return false, true
	}
	if strings.Contains(normalized, "connect") {
		return true, true
	}
	return false, false
}

var (
	unknownStateMu   sync.Mutex
	unknownStateSeen = map[string]struct{}{}
)

// noteUnknownNetshStates logs unrecognized State words once each. The snapshot
// is collected every few seconds, so logging on every collection would flood
// the log on a Windows language the table does not cover yet.
func noteUnknownNetshStates(words []string) {
	unknownStateMu.Lock()
	defer unknownStateMu.Unlock()
	for _, word := range words {
		if _, ok := unknownStateSeen[word]; ok {
			continue
		}
		unknownStateSeen[word] = struct{}{}
		log.Printf("netsh: unrecognized interface state %q; falling back to the adapter status", word)
	}
}

// netshInterfaceState is the parsed result of "netsh interface ipvX show
// interface": adapter names mapped to their connected state, plus the interface
// index mapping needed to interpret the route table.
type netshInterfaceState struct {
	Connected map[string]bool
	// Known is false for adapters whose state word could not be translated.
	Known map[string]bool
	Index map[string]string
	// UnknownWords lists the state words that were not recognized.
	UnknownWords []string
}

func newNetshInterfaceState() netshInterfaceState {
	return netshInterfaceState{
		Connected: map[string]bool{},
		Known:     map[string]bool{},
		Index:     map[string]string{},
	}
}

// parseNetshInterfaceList parses "netsh interface ipvX show interface".
//
// Columns are positional (Idx, Met, MTU, State, Name), which is locale
// independent. The header row is skipped by requiring a numeric index instead
// of matching the localized "Idx" label, and the state word goes through
// netshStateConnected instead of being compared with "connected".
func parseNetshInterfaceList(raw string) netshInterfaceState {
	state := newNetshInterfaceState()
	unknown := map[string]struct{}{}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSuffix(strings.TrimPrefix(line, "\ufeff"), "\r")
		fields := netshColumns.Split(strings.TrimSpace(line), -1)
		if len(fields) < 5 {
			continue
		}
		// The header and the separator row are skipped by requiring a numeric
		// interface index; matching the localized "Idx" label would not work.
		if _, err := strconv.Atoi(strings.TrimSpace(fields[0])); err != nil {
			continue
		}
		// The name is last and may contain single spaces, so everything after
		// the state column belongs to it.
		name := strings.Join(fields[4:], " ")
		if name == "" {
			continue
		}
		index := strings.TrimSpace(fields[0])
		state.Index[index] = name

		word := strings.TrimSpace(fields[3])
		connected, known := netshStateConnected(word)
		state.Connected[name] = connected
		state.Known[name] = known
		if !known {
			if _, seen := unknown[word]; !seen {
				unknown[word] = struct{}{}
				state.UnknownWords = append(state.UnknownWords, word)
			}
		}
	}
	return state
}

// netshColumns splits a netsh table row on runs of two or more spaces, which is
// how netsh separates its columns.
//
// A plain strings.Fields split is not usable here: a localized state word may
// contain a space ("Nicht verbunden"), and whitespace splitting shifted part of
// it into the adapter name, producing adapters called "verbunden Ethernet".
// Single spaces stay inside a column, so both cases parse correctly.
var netshColumns = regexp.MustCompile(`\s{2,}`)

// isWarpAdapterName reports whether the adapter name refers to the Cloudflare WARP tunnel.
func isWarpAdapterName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(n, "warp") || strings.Contains(n, "cloudflare")
}

// defaultRouteCandidate holds one default-route entry parsed from the route table.
type defaultRouteCandidate struct {
	name    string
	gateway string
	metric  int
	isWarp  bool
}

// netshDefaultRoute is the default route that should be treated as the
// physical one, together with the gateway it points at.
type netshDefaultRoute struct {
	Interface string
	Gateway   string
	Metric    int
}

// pickDefaultRoute returns the best interface name among default-route candidates.
// Preference order: non-WARP over WARP, then lower metric (higher priority).
func pickDefaultRoute(candidates []defaultRouteCandidate) defaultRouteCandidate {
	if len(candidates) == 0 {
		return defaultRouteCandidate{}
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
	return best
}

// parseNetshDefaultRoute parses a route table ("netsh interface ipv4|ipv6 show
// route") and returns the interface plus gateway of the default route. When
// WARP is active it also installs a default route; the non-WARP interface is
// preferred so downstream logic does not target the tunnel adapter.
//
// The parser is positional: the metric must be numeric and the interface index
// must resolve, which keeps localized headers and separators out without
// matching any literal.
func parseNetshDefaultRoute(raw, prefix string, ifaceMap map[string]string) netshDefaultRoute {
	var candidates []defaultRouteCandidate

	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "\ufeff")))
		if len(fields) < 5 {
			continue
		}
		for i, field := range fields {
			if field != prefix {
				continue
			}
			// Layout: ... Met Prefix Idx Gateway ...
			if i < 1 || i+1 >= len(fields) {
				continue
			}
			metric, err := strconv.Atoi(fields[i-1])
			if err != nil {
				continue
			}
			name, ok := ifaceMap[fields[i+1]]
			if !ok || name == "" {
				continue
			}
			gateway := ""
			if i+2 < len(fields) && isIPToken(fields[i+2]) {
				gateway = fields[i+2]
			}
			candidates = append(candidates, defaultRouteCandidate{
				name:    name,
				gateway: gateway,
				metric:  metric,
				isWarp:  isWarpAdapterName(name),
			})
		}
	}

	best := pickDefaultRoute(candidates)
	return netshDefaultRoute{Interface: best.name, Gateway: best.gateway, Metric: best.metric}
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

		// Section header. The label is localized ("Configuration for interface"
		// / 接口 "WiFi" 的配置), but every locale quotes the interface name, so
		// the quoted token identifies the section instead of the literal.
		if name := extractQuotedString(line); name != "" {
			entries = append(entries, netshDNSEntry{Name: name})
			current = &entries[len(entries)-1]
			continue
		}

		if current == nil {
			continue
		}

		// A line that is nothing but an address is a continuation line. This is
		// checked first because IPv6 addresses contain colons, and the key/value
		// branch below would otherwise slice one into an unparsable value.
		if isIPToken(line) {
			current.Servers = append(current.Servers, line)
			continue
		}

		// Otherwise accept any "label: value" line whose value is an address.
		// The label is localized ("DNS servers configured through DHCP" /
		// "通过 DHCP 配置的 DNS 服务器"), so it is not matched literally; the
		// address check drops "None"/"无" and the suffix label on its own.
		if idx := strings.Index(line, ":"); idx >= 0 {
			if value := strings.TrimSpace(line[idx+1:]); isIPToken(value) {
				current.Servers = append(current.Servers, value)
			}
		}
	}
	return entries
}

// isIPToken reports whether value looks like an IP address. Addresses with a
// zone suffix (fe80::1%12) and bracketed IPv6 addresses are accepted. Empty
// values and the "None" placeholder netsh prints for an unconfigured family
// are rejected.
func isIPToken(value string) bool {
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

// netshGetInterfaces runs "netsh interface ipvX show interface" once and returns
// both the connected state and the index mapping, so callers do not have to run
// the same command again for the route table.
func netshGetInterfaces(ctx context.Context, family string) (netshInterfaceState, error) {
	raw, err := execWithTimeout(ctx, "netsh", "interface", family, "show", "interface")
	if err != nil && strings.TrimSpace(raw) == "" {
		return newNetshInterfaceState(), err
	}
	state := parseNetshInterfaceList(raw)
	noteUnknownNetshStates(state.UnknownWords)
	return state, nil
}

// netshGetDefaultRoute gets the default route (and its gateway) for a family.
func netshGetDefaultRoute(ctx context.Context, family, prefix string, ifaceMap map[string]string) (netshDefaultRoute, error) {
	routeRaw, err := execWithTimeout(ctx, "netsh", "interface", family, "show", "route")
	if err != nil && strings.TrimSpace(routeRaw) == "" {
		return netshDefaultRoute{}, err
	}
	return parseNetshDefaultRoute(routeRaw, prefix, ifaceMap), nil
}

// netshGetDNSServers gets DNS servers for all adapters.
func netshGetDNSServers(ctx context.Context, family string) ([]netshDNSEntry, error) {
	raw, err := execWithTimeout(ctx, "netsh", "interface", family, "show", "dnsservers")
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	return parseNetshDNSServers(raw), nil
}
