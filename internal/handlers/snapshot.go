package handlers

import (
	"context"
	"fmt"
	"log"
	"net"
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

type adapterBinding struct {
	Name        string `json:"Name"`
	ComponentID string `json:"ComponentID"`
	Enabled     bool   `json:"Enabled"`
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

type ipConfigInfo struct {
	InterfaceAlias string `json:"InterfaceAlias"`
	IPv4Gateway    string `json:"IPv4Gateway"`
	IPv6Gateway    string `json:"IPv6Gateway"`
}

type dnsInfo struct {
	InterfaceAlias  string `json:"InterfaceAlias"`
	ServerAddresses any    `json:"ServerAddresses"`
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

func getDefaultRouteAliases(ctx context.Context, family, prefix string) ([]string, error) {
	cmd := fmt.Sprintf("(Get-NetRoute -AddressFamily %s -DestinationPrefix '%s' -ErrorAction SilentlyContinue | Sort-Object RouteMetric, InterfaceMetric | Select-Object -ExpandProperty InterfaceAlias -Unique) | ConvertTo-Json -Compress", family, prefix)
	raw, err := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", cmd)
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	items, err := decodeJSONList[string](raw)
	if err != nil {
		return nil, err
	}
	aliases := make([]string, 0, len(items))
	for _, item := range items {
		if s := strings.TrimSpace(item); s != "" {
			aliases = append(aliases, s)
		}
	}
	return aliases, nil
}

func collectNetworkSnapshot() (networkSnapshot, error) {
	baseCtx := context.Background()

	var (
		basicRaw     string
		bindingRaw   string
		ipCfgRaw     string
		dnsRaw       string
		defaultV4    []string
		defaultV6    []string
		warpStatus   warpSnapshot
		warpSettings warpSettingsSnapshot
		tcpProbe     tcpProbeSnapshot
	)

	var wg sync.WaitGroup
	wg.Add(9)
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
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		bindingCmd := "Get-NetAdapterBinding | Where-Object { $_.ComponentID -in @('ms_tcpip','ms_tcpip6') } | Select-Object Name, ComponentID, Enabled | ConvertTo-Json -Compress"
		var err error
		bindingRaw, err = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", bindingCmd)
		if err != nil {
			log.Printf("snapshot: Get-NetAdapterBinding failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		ipCfgCmd := "Get-NetIPConfiguration | Select-Object InterfaceAlias, @{Name='IPv4Gateway';Expression={$_.IPv4DefaultGateway.NextHop}}, @{Name='IPv6Gateway';Expression={$_.IPv6DefaultGateway.NextHop}} | ConvertTo-Json -Compress"
		var err error
		ipCfgRaw, err = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ipCfgCmd)
		if err != nil {
			log.Printf("snapshot: Get-NetIPConfiguration failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		dnsCmd := "Get-DnsClientServerAddress | Select-Object InterfaceAlias, ServerAddresses | ConvertTo-Json -Compress"
		var err error
		dnsRaw, err = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", dnsCmd)
		if err != nil {
			log.Printf("snapshot: Get-DnsClientServerAddress failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		defaultV4, _ = getDefaultRouteAliases(ctx, "IPv4", "0.0.0.0/0")
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		defaultV6, _ = getDefaultRouteAliases(ctx, "IPv6", "::/0")
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutMedium)
		defer cancel()
		warpStatus = probeWarpStatus(ctx)
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		warpSettings = probeWarpSettings(ctx)
	}()
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(baseCtx, timeoutShort)
		defer cancel()
		tcpProbe = probeCloudflareTCP(ctx)
	}()
	wg.Wait()

	basics, basicsErr := decodeJSONList[adapterBasic](basicRaw)
	bindings, bindingsErr := decodeJSONList[adapterBinding](bindingRaw)
	if bindingsErr != nil {
		log.Printf("snapshot: decode bindings failed: %v", bindingsErr)
	}
	ipConfigs, ipCfgErr := decodeJSONList[ipConfigInfo](ipCfgRaw)
	if ipCfgErr != nil {
		log.Printf("snapshot: decode ipConfigs failed: %v", ipCfgErr)
	}
	dnsInfos, dnsErr := decodeJSONList[dnsInfo](dnsRaw)
	if dnsErr != nil {
		log.Printf("snapshot: decode dnsInfos failed: %v", dnsErr)
	}

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
			CollectedAt: time.Now().Format(time.RFC3339),
			Warp:        warpStatus,
			WarpSettings: warpSettings,
		}, fmt.Errorf("adapter list unavailable: %w", basicsErr)
	}

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

	bindingMap := make(map[string]map[string]bool)
	for _, b := range bindings {
		m, ok := bindingMap[b.Name]
		if !ok {
			m = map[string]bool{}
			bindingMap[b.Name] = m
		}
		m[b.ComponentID] = b.Enabled
	}

	ipCfgMap := make(map[string]ipConfigInfo)
	for _, cfg := range ipConfigs {
		if strings.TrimSpace(cfg.InterfaceAlias) == "" {
			continue
		}
		ipCfgMap[cfg.InterfaceAlias] = cfg
	}

	dnsMap := make(map[string][]string)
	for _, info := range dnsInfos {
		alias := strings.TrimSpace(info.InterfaceAlias)
		if alias == "" {
			continue
		}
		servers := normalizeStringSlice(info.ServerAddresses)
		if len(servers) == 0 {
			continue
		}
		existing := make(map[string]struct{}, len(dnsMap[alias]))
		for _, s := range dnsMap[alias] {
			existing[s] = struct{}{}
		}
		for _, s := range servers {
			if _, ok := existing[s]; ok {
				continue
			}
			dnsMap[alias] = append(dnsMap[alias], s)
			existing[s] = struct{}{}
		}
	}

	basicNameSet := make(map[string]struct{}, len(basics))
	for _, b := range basics {
		basicNameSet[b.Name] = struct{}{}
	}

	selected := make(map[string]struct{})
	for _, name := range append(defaultV4, defaultV6...) {
		if _, ok := basicNameSet[name]; ok {
			selected[name] = struct{}{}
		}
	}
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
