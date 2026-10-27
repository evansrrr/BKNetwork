package handlers

import (
	"context"
	"fmt"
	"log"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type adapterBasic struct {
	Name                 string `json:"Name"`
	Status               string `json:"Status"`
	MacAddress           string `json:"MacAddress"`
	InterfaceDescription string `json:"InterfaceDescription"`
}

type dnsInfo struct {
	InterfaceAlias  string `json:"InterfaceAlias"`
	ServerAddresses any    `json:"ServerAddresses"`
}

type adapterSnapshot struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Description string   `json:"description,omitempty"`
	MacAddress  string   `json:"macAddress,omitempty"`
	IPv4Enabled bool     `json:"ipv4Enabled"`
	IPv6Enabled bool     `json:"ipv6Enabled"`
	FreeFlow    bool     `json:"freeFlow"`
	IPv4Gateway string   `json:"ipv4Gateway,omitempty"`
	IPv6Gateway string   `json:"ipv6Gateway,omitempty"`
	DNS         []string `json:"dns"`
	IPv4        []string `json:"ipv4"`
	IPv6        []string `json:"ipv6"`
}

type tcpProbeSnapshot struct {
	Target    string `json:"target"`
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	CheckedAt string `json:"checkedAt"`
	Error     string `json:"error,omitempty"`
}

type warpSnapshot struct {
	Connected bool   `json:"connected"`
	Status    string `json:"status,omitempty"`
	CheckedAt string `json:"checkedAt"`
	Raw       string `json:"raw,omitempty"`
	Error     string `json:"error,omitempty"`
}

type warpSettingsSnapshot struct {
	CheckedAt      string `json:"checkedAt"`
	Mode           string `json:"mode,omitempty"`
	TunnelProtocol string `json:"tunnelProtocol,omitempty"`
	Error          string `json:"error,omitempty"`
}

type networkSnapshot struct {
	CollectedAt       string               `json:"collectedAt"`
	Online            bool                 `json:"online"`
	Adapters          []adapterSnapshot    `json:"adapters"`
	AvailableAdapters []string             `json:"availableAdapters"`
	CloudflareTCP     tcpProbeSnapshot     `json:"cloudflareTcp"`
	Warp              warpSnapshot         `json:"warp"`
	WarpSettings      warpSettingsSnapshot `json:"warpSettings"`
}

func normalizeStringSlice(v any) []string {
	if v == nil {
		return []string{}
	}
	out := make([]string, 0)
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
	case []string:
		for _, s := range t {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	case string:
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// adapterGateway holds the gateways taken from the default routes. The old code
// read them from "netsh interface ipv4 show config", whose labels are
// localized; the route table already carries the same address and is collected
// anyway, so that process is gone.
type adapterGateway struct {
	IPv4 string
	IPv6 string
}

func collectNetworkSnapshot() (networkSnapshot, error) {
	if runtime.GOOS == "darwin" {
		return collectDarwinNetworkSnapshot()
	}
	baseCtx := context.Background()

	var (
		metas        []adapterMeta
		metaErr      error
		ipv4State    netshInterfaceState
		ipv6State    netshInterfaceState
		ipv4Route    netshDefaultRoute
		ipv6Route    netshDefaultRoute
		dnsIPv4      []netshDNSEntry
		dnsIPv6      []netshDNSEntry
		warpStatus   warpSnapshot
		warpSettings warpSettingsSnapshot
		tcpProbe     tcpProbeSnapshot
	)

	// Phase 1: queries that do not depend on each other.
	var wg sync.WaitGroup
	wg.Add(8)
	// Adapter metadata (name, MAC, description) is served from a cache and
	// refreshed in the background, so a snapshot no longer blocks on the
	// external adapter query every time.
	go func() {
		defer wg.Done()
		metas, metaErr = adapterMetadataList(baseCtx)
		if metaErr != nil {
			log.Printf("snapshot: adapter metadata failed: %v", metaErr)
		}
	}()
	// netsh: interface list for both families. The same output also provides the
	// index mapping the route parser needs, so it is fetched only once.
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv4State, err = netshGetInterfaces(ctx, "ipv4")
		if err != nil {
			log.Printf("snapshot: netsh IPv4 interface list failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv6State, err = netshGetInterfaces(ctx, "ipv6")
		if err != nil {
			log.Printf("snapshot: netsh IPv6 interface list failed: %v", err)
		}
	}()
	// netsh: DNS servers (IPv4 + IPv6)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		dnsIPv4, err = netshGetDNSServers(ctx, "ipv4")
		if err != nil {
			log.Printf("snapshot: netsh IPv4 DNS failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		dnsIPv6, err = netshGetDNSServers(ctx, "ipv6")
		if err != nil {
			log.Printf("snapshot: netsh IPv6 DNS failed: %v", err)
		}
	}()
	// warp-cli: status
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutMedium)
		defer cancel()
		warpStatus = probeWarpStatus(ctx)
	}()
	// warp-cli: settings
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		warpSettings = probeWarpSettings(ctx)
	}()
	// TCP probe (native Go, no process)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		tcpProbe = cachedCloudflareTCP(ctx)
	}()
	wg.Wait()

	// Phase 2: the default route lookup reuses the interface index mapping from
	// phase 1 instead of running "show interface" a second time.
	var routeWG sync.WaitGroup
	routeWG.Add(2)
	go func() {
		defer routeWG.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv4Route, err = netshGetDefaultRoute(ctx, "ipv4", "0.0.0.0/0", ipv4State.Index)
		if err != nil {
			log.Printf("snapshot: netsh IPv4 route failed: %v", err)
		}
	}()
	go func() {
		defer routeWG.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv6Route, err = netshGetDefaultRoute(ctx, "ipv6", "::/0", ipv6State.Index)
		if err != nil {
			log.Printf("snapshot: netsh IPv6 route failed: %v", err)
		}
	}()
	routeWG.Wait()

	if len(metas) == 0 {
		return networkSnapshot{
			CollectedAt:  time.Now().Format(time.RFC3339),
			Warp:         warpStatus,
			WarpSettings: warpSettings,
		}, fmt.Errorf("adapter list unavailable: %w", metaErr)
	}

	// Adapters netsh knows about but the cached metadata does not (plugged in
	// after the last refresh) are added so they show up immediately.
	basics := mergeAdapterMetadata(metas, ipv4State, ipv6State)

	// Build address maps from the OS.
	ipv4Map := make(map[string][]string)
	ipv6Map := make(map[string][]string)
	ifs, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifs {
			addrs, addrErr := iface.Addrs()
			if addrErr != nil {
				continue
			}
			for _, addr := range addrs {
				ip, _, parseErr := net.ParseCIDR(addr.String())
				if parseErr != nil {
					continue
				}
				if ip.To4() != nil {
					ipv4Map[iface.Name] = append(ipv4Map[iface.Name], ip.String())
				} else if ip.To16() != nil {
					ipv6Map[iface.Name] = append(ipv6Map[iface.Name], ip.String())
				}
			}
		}
	}

	// Binding state per family.
	bindingMap := make(map[string]map[string]bool)
	for name, enabled := range ipv4State.Connected {
		if bindingMap[name] == nil {
			bindingMap[name] = make(map[string]bool)
		}
		bindingMap[name]["ms_tcpip"] = enabled
	}
	for name, enabled := range ipv6State.Connected {
		if bindingMap[name] == nil {
			bindingMap[name] = make(map[string]bool)
		}
		bindingMap[name]["ms_tcpip6"] = enabled
	}

	// Gateways come from the default routes of both families.
	gateways := make(map[string]adapterGateway)
	if name := ipv4Route.Interface; name != "" {
		entry := gateways[name]
		entry.IPv4 = ipv4Route.Gateway
		gateways[name] = entry
	}
	if name := ipv6Route.Interface; name != "" {
		entry := gateways[name]
		entry.IPv6 = ipv6Route.Gateway
		gateways[name] = entry
	}

	dnsMap := make(map[string][]string)
	for _, entry := range dnsIPv4 {
		name := strings.TrimSpace(entry.Name)
		if name != "" {
			dnsMap[name] = mergeUniqueStrings(dnsMap[name], entry.Servers)
		}
	}
	for _, entry := range dnsIPv6 {
		name := strings.TrimSpace(entry.Name)
		if name != "" {
			dnsMap[name] = mergeUniqueStrings(dnsMap[name], entry.Servers)
		}
	}

	basicNameSet := make(map[string]struct{}, len(basics))
	for _, b := range basics {
		basicNameSet[b.Name] = struct{}{}
	}

	selected := make(map[string]struct{})
	// Adapters that own a default route.
	for _, name := range []string{ipv4Route.Interface, ipv6Route.Interface} {
		if name == "" {
			continue
		}
		if _, ok := basicNameSet[name]; ok {
			selected[name] = struct{}{}
		}
	}
	// Adapters that are UP and have a gateway.
	for _, b := range basics {
		gateway := gateways[b.Name]
		statusUp := strings.EqualFold(strings.TrimSpace(deriveAdapterStatus(b, ipv4State, ipv6State)), "up")
		if statusUp && (gateway.IPv4 != "" || gateway.IPv6 != "") {
			selected[b.Name] = struct{}{}
		}
	}
	if _, ok := basicNameSet["CloudflareWARP"]; ok {
		selected["CloudflareWARP"] = struct{}{}
	}

	availableAdapters := make([]string, 0, len(basics))
	for _, b := range basics {
		if name := strings.TrimSpace(b.Name); name != "" {
			availableAdapters = append(availableAdapters, name)
		}
	}
	sort.Strings(availableAdapters)

	adapters := make([]adapterSnapshot, 0, len(basics))
	for _, b := range basics {
		if _, ok := selected[b.Name]; !ok {
			continue
		}
		adapterBindings := bindingMap[b.Name]
		gateway := gateways[b.Name]
		ipv4 := ipv4Map[b.Name]
		ipv6 := ipv6Map[b.Name]
		dns := dnsMap[b.Name]
		if ipv4 == nil {
			ipv4 = []string{}
		}
		if ipv6 == nil {
			ipv6 = []string{}
		}
		if dns == nil {
			dns = []string{}
		}
		adapters = append(adapters, adapterSnapshot{
			Name:        b.Name,
			Status:      deriveAdapterStatus(b, ipv4State, ipv6State),
			Description: b.Description,
			MacAddress:  b.MacAddress,
			IPv4Enabled: adapterBindings["ms_tcpip"],
			IPv6Enabled: adapterBindings["ms_tcpip6"],
			FreeFlow:    false,
			IPv4Gateway: gateway.IPv4,
			IPv6Gateway: gateway.IPv6,
			DNS:         dns,
			IPv4:        ipv4,
			IPv6:        ipv6,
		})
	}

	for i := range adapters {
		adapters[i].FreeFlow = warpStatus.Connected && adapters[i].IPv6Enabled && !adapters[i].IPv4Enabled
	}

	online := hasOnlineAdapter(adapters)

	sort.Slice(adapters, func(i, j int) bool {
		return strings.ToLower(adapters[i].Name) < strings.ToLower(adapters[j].Name)
	})

	return networkSnapshot{
		CollectedAt:       time.Now().Format(time.RFC3339),
		Online:            online,
		Adapters:          adapters,
		AvailableAdapters: availableAdapters,
		CloudflareTCP:     tcpProbe,
		Warp:              warpStatus,
		WarpSettings:      warpSettings,
	}, nil
}

// mergeUniqueStrings appends the values of extra that are not in base yet,
// keeping the original order (IPv4 servers before IPv6 servers).
func mergeUniqueStrings(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, value := range base {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range extra {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func hasOnlineAdapter(adapters []adapterSnapshot) bool {
	for _, adapter := range adapters {
		if !strings.EqualFold(strings.TrimSpace(adapter.Status), "up") {
			continue
		}
		if len(adapter.IPv4) > 0 || len(adapter.IPv6) > 0 {
			return true
		}
		if strings.TrimSpace(adapter.IPv4Gateway) != "" || strings.TrimSpace(adapter.IPv6Gateway) != "" {
			return true
		}
	}
	return false
}

const (
	// tcpProbeSuccessTTL and tcpProbeFailureTTL keep the outbound probe off the
	// snapshot path. Every collection used to dial cloudflare.com:443 and wait
	// for the 1.2s dial timeout when the network blocks it, which made a warm
	// snapshot take 1.3s even after the adapter query had been cached.
	tcpProbeSuccessTTL = 30 * time.Second
	tcpProbeFailureTTL = 10 * time.Second
)

var (
	tcpProbeMu   sync.Mutex
	tcpProbeLast tcpProbeSnapshot
	tcpProbeAt   time.Time
)

// cachedCloudflareTCP returns the last probe result while it is recent enough,
// so the snapshot does not block on an outbound dial every few seconds.
func cachedCloudflareTCP(ctx context.Context) tcpProbeSnapshot {
	tcpProbeMu.Lock()
	last, at := tcpProbeLast, tcpProbeAt
	tcpProbeMu.Unlock()

	if !at.IsZero() {
		ttl := tcpProbeSuccessTTL
		if !last.OK {
			ttl = tcpProbeFailureTTL
		}
		if time.Since(at) < ttl {
			return last
		}
	}

	result := probeCloudflareTCP(ctx)

	tcpProbeMu.Lock()
	tcpProbeLast, tcpProbeAt = result, time.Now()
	tcpProbeMu.Unlock()
	return result
}

func probeCloudflareTCP(ctx context.Context) tcpProbeSnapshot {
	result := tcpProbeSnapshot{
		Target:    "cloudflare.com:443",
		CheckedAt: time.Now().Format(time.RFC3339),
	}
	start := time.Now()
	dialer := net.Dialer{Timeout: timeoutDial}
	conn, err := dialer.DialContext(ctx, "tcp", result.Target)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.LatencyMs = time.Since(start).Milliseconds()
	_ = conn.Close()
	return result
}
