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

func collectNetworkSnapshot() (networkSnapshot, error) {
	if runtime.GOOS == "darwin" {
		return collectDarwinNetworkSnapshot()
	}
	baseCtx := context.Background()

	var (
		basicRaw         string
		ipv4Binding      map[string]bool
		ipv6Binding      map[string]bool
		ipv4Configs      []netshIPConfig
		ipv4DefaultRoute string
		ipv6DefaultRoute string
		dnsIPv4          []netshDNSEntry
		dnsIPv6          []netshDNSEntry
		warpStatus       warpSnapshot
		warpSettings     warpSettingsSnapshot
		tcpProbe         tcpProbeSnapshot
	)

	var wg sync.WaitGroup
	wg.Add(11)
	// 1. PowerShell: Get-NetAdapter (保留，需要 MAC 和描述信息)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		basicCmd := "Get-NetAdapter | Select-Object Name, Status, MacAddress, InterfaceDescription | ConvertTo-Json -Compress"
		var err error
		basicRaw, err = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", basicCmd)
		if err != nil {
			log.Printf("snapshot: Get-NetAdapter failed: %v", err)
		}
	}()
	// 2. netsh: IPv4 binding state
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv4Binding, err = netshGetIPv4Binding(ctx)
		if err != nil {
			log.Printf("snapshot: netsh IPv4 binding failed: %v", err)
		}
	}()
	// 3. netsh: IPv6 binding state
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv6Binding, err = netshGetIPv6Binding(ctx)
		if err != nil {
			log.Printf("snapshot: netsh IPv6 binding failed: %v", err)
		}
	}()
	// 4. netsh: IPv4 config (gateway)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv4Configs, err = netshGetIPv4Config(ctx)
		if err != nil {
			log.Printf("snapshot: netsh IPv4 config failed: %v", err)
		}
	}()
	// 5. netsh: IPv4 default route
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv4DefaultRoute, err = netshGetIPv4DefaultRoute(ctx)
		if err != nil {
			log.Printf("snapshot: netsh IPv4 route failed: %v", err)
		}
	}()
	// 6. netsh: IPv6 default route
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		var err error
		ipv6DefaultRoute, err = netshGetIPv6DefaultRoute(ctx)
		if err != nil {
			log.Printf("snapshot: netsh IPv6 route failed: %v", err)
		}
	}()
	// 6. netsh: DNS servers (IPv4 + IPv6)
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
	// 7. warp-cli: status
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutMedium)
		defer cancel()
		warpStatus = probeWarpStatus(ctx)
	}()
	// 8. warp-cli: settings
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		warpSettings = probeWarpSettings(ctx)
	}()
	// 9. TCP probe (native Go, no process)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		tcpProbe = probeCloudflareTCP(ctx)
	}()
	wg.Wait()

	basics, basicsErr := decodeJSONList[adapterBasic](basicRaw)

	if (basicsErr != nil || len(basics) == 0) && basicRaw == "" {
		retryCtx, retryCancel := context.WithTimeout(baseCtx, timeoutShort)
		defer retryCancel()
		var retryErr error
		basicRaw, retryErr = execWithTimeout(retryCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			"Get-NetAdapter | Select-Object Name, Status, MacAddress, InterfaceDescription | ConvertTo-Json -Compress")
		if retryErr != nil {
			log.Printf("snapshot: retry Get-NetAdapter failed: %v", retryErr)
		}
		basics, basicsErr = decodeJSONList[adapterBasic](basicRaw)
	}

	if basicsErr != nil || len(basics) == 0 {
		return networkSnapshot{
			CollectedAt:  time.Now().Format(time.RFC3339),
			Warp:         warpStatus,
			WarpSettings: warpSettings,
		}, fmt.Errorf("adapter list unavailable: %w", basicsErr)
	}

	// Build lookup maps from netsh results
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

	// Build binding map from netsh results
	bindingMap := make(map[string]map[string]bool)
	for name, enabled := range ipv4Binding {
		if bindingMap[name] == nil {
			bindingMap[name] = make(map[string]bool)
		}
		bindingMap[name]["ms_tcpip"] = enabled
	}
	for name, enabled := range ipv6Binding {
		if bindingMap[name] == nil {
			bindingMap[name] = make(map[string]bool)
		}
		bindingMap[name]["ms_tcpip6"] = enabled
	}

	// Build IP config map from netsh results
	ipCfgMap := make(map[string]netshIPConfig)
	for _, cfg := range ipv4Configs {
		if strings.TrimSpace(cfg.Name) == "" {
			continue
		}
		ipCfgMap[cfg.Name] = cfg
	}
	// Add IPv6 gateway from route table (ipv6DefaultRoute is the interface name with default route)
	if ipv6DefaultRoute != "" {
		if cfg, ok := ipCfgMap[ipv6DefaultRoute]; ok {
			// IPv6 gateway is the link-local gateway from the route table
			// We'll leave it empty for now since netsh doesn't easily provide this
			ipCfgMap[ipv6DefaultRoute] = cfg
		}
	}

	// Build DNS map from netsh results
	dnsMap := make(map[string][]string)
	for _, entry := range dnsIPv4 {
		alias := strings.TrimSpace(entry.Name)
		if alias == "" || len(entry.Servers) == 0 {
			continue
		}
		existing := make(map[string]struct{}, len(dnsMap[alias]))
		for _, s := range dnsMap[alias] {
			existing[s] = struct{}{}
		}
		for _, s := range entry.Servers {
			if _, ok := existing[s]; ok {
				continue
			}
			dnsMap[alias] = append(dnsMap[alias], s)
			existing[s] = struct{}{}
		}
	}
	for _, entry := range dnsIPv6 {
		alias := strings.TrimSpace(entry.Name)
		if alias == "" || len(entry.Servers) == 0 {
			continue
		}
		existing := make(map[string]struct{}, len(dnsMap[alias]))
		for _, s := range dnsMap[alias] {
			existing[s] = struct{}{}
		}
		for _, s := range entry.Servers {
			if _, ok := existing[s]; ok {
				continue
			}
			dnsMap[alias] = append(dnsMap[alias], s)
			existing[s] = struct{}{}
		}
	}

	// Determine selected adapters
	basicNameSet := make(map[string]struct{}, len(basics))
	for _, b := range basics {
		basicNameSet[b.Name] = struct{}{}
	}

	selected := make(map[string]struct{})
	// Select adapters with default routes (already resolved to interface names by netsh)
	if ipv4DefaultRoute != "" {
		if _, ok := basicNameSet[ipv4DefaultRoute]; ok {
			selected[ipv4DefaultRoute] = struct{}{}
		}
	}
	if ipv6DefaultRoute != "" {
		if _, ok := basicNameSet[ipv6DefaultRoute]; ok {
			selected[ipv6DefaultRoute] = struct{}{}
		}
	}
	// Select adapters that are UP and have a gateway
	for _, b := range basics {
		cfg, ok := ipCfgMap[b.Name]
		if !ok {
			continue
		}
		statusUp := strings.EqualFold(strings.TrimSpace(b.Status), "up")
		hasGateway := strings.TrimSpace(cfg.IPv4Gateway) != "" || strings.TrimSpace(cfg.IPv6Gateway) != ""
		if statusUp && hasGateway {
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
		cfg := ipCfgMap[b.Name]
		ipv4 := ipv4Map[b.Name]
		ipv6 := ipv6Map[b.Name]
		dns := dnsMap[b.Name]
		if ipv4 == nil {
			ipv4 = []string{}
		}
		if ipv6 == nil {
			ipv6 = []string{}
		}
		adapters = append(adapters, adapterSnapshot{
			Name:        b.Name,
			Status:      b.Status,
			Description: b.InterfaceDescription,
			MacAddress:  b.MacAddress,
			IPv4Enabled: adapterBindings["ms_tcpip"],
			IPv6Enabled: adapterBindings["ms_tcpip6"],
			FreeFlow:    false,
			IPv4Gateway: strings.TrimSpace(cfg.IPv4Gateway),
			IPv6Gateway: strings.TrimSpace(cfg.IPv6Gateway),
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
