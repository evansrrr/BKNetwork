package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"bknetwork/internal/events"
	appsettings "bknetwork/internal/settings"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/windows"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type apiResponse struct {
	OK      bool        `json:"ok"`
	Error   string      `json:"error,omitempty"`
	Detail  string      `json:"detail,omitempty"`
	Output  string      `json:"output,omitempty"`
	Payload interface{} `json:"payload,omitempty"`
}

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

type settingsSnapshot struct {
	AutoStart        bool `json:"autoStart"`
	SilentStart      bool `json:"silentStart"`
	WarpAutoStart    bool `json:"warpAutoStart"`
	WarpAppAutoStart bool `json:"warpAppAutoStart"`
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
	default:
		// ignore unknown shape
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

func writeJSON(w http.ResponseWriter, v interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSONList[T any](raw string) ([]T, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []T{}, nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []T
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var one T
	if err := json.Unmarshal([]byte(raw), &one); err != nil {
		return nil, err
	}
	return []T{one}, nil
}

func collectNetworkSnapshot() (networkSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	var (
		basicRaw     string
		basicErr     error
		bindingRaw   string
		bindingErr   error
		ipCfgRaw     string
		ipCfgErr     error
		dnsRaw       string
		dnsErr       error
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
		basicCmd := "Get-NetAdapter | Select-Object Name, Status, MacAddress, InterfaceDescription | ConvertTo-Json -Compress"
		basicRaw, basicErr = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", basicCmd)
	}()
	go func() {
		defer wg.Done()
		bindingCmd := "Get-NetAdapterBinding | Where-Object { $_.ComponentID -in @('ms_tcpip','ms_tcpip6') } | Select-Object Name, ComponentID, Enabled | ConvertTo-Json -Compress"
		bindingRaw, bindingErr = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", bindingCmd)
	}()
	go func() {
		defer wg.Done()
		ipCfgCmd := "Get-NetIPConfiguration | Select-Object InterfaceAlias, @{Name='IPv4Gateway';Expression={$_.IPv4DefaultGateway.NextHop}}, @{Name='IPv6Gateway';Expression={$_.IPv6DefaultGateway.NextHop}} | ConvertTo-Json -Compress"
		ipCfgRaw, ipCfgErr = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ipCfgCmd)
	}()
	go func() {
		defer wg.Done()
		dnsCmd := "Get-DnsClientServerAddress | Select-Object InterfaceAlias, ServerAddresses | ConvertTo-Json -Compress"
		dnsRaw, dnsErr = execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", dnsCmd)
	}()
	go func() {
		defer wg.Done()
		defaultV4, _ = getDefaultRouteAliases(ctx, "IPv4", "0.0.0.0/0")
	}()
	go func() {
		defer wg.Done()
		defaultV6, _ = getDefaultRouteAliases(ctx, "IPv6", "::/0")
	}()
	go func() {
		defer wg.Done()
		warpStatus = probeWarpStatus(ctx)
	}()
	go func() {
		defer wg.Done()
		warpSettings = probeWarpSettings(ctx)
	}()
	go func() {
		defer wg.Done()
		tcpProbe = probeCloudflareTCP(ctx)
	}()
	wg.Wait()

	if basicErr != nil && strings.TrimSpace(basicRaw) == "" {
		return networkSnapshot{}, fmt.Errorf("get adapter list failed: %w", basicErr)
	}
	basics, err := decodeJSONList[adapterBasic](basicRaw)
	if err != nil {
		return networkSnapshot{}, fmt.Errorf("parse adapter list failed: %w", err)
	}

	if bindingErr != nil && strings.TrimSpace(bindingRaw) == "" {
		return networkSnapshot{}, fmt.Errorf("get adapter bindings failed: %w", bindingErr)
	}
	bindings, err := decodeJSONList[adapterBinding](bindingRaw)
	if err != nil {
		return networkSnapshot{}, fmt.Errorf("parse adapter bindings failed: %w", err)
	}

	if ipCfgErr != nil && strings.TrimSpace(ipCfgRaw) == "" {
		return networkSnapshot{}, fmt.Errorf("get ip configuration failed: %w", ipCfgErr)
	}
	ipConfigs, err := decodeJSONList[ipConfigInfo](ipCfgRaw)
	if err != nil {
		return networkSnapshot{}, fmt.Errorf("parse ip configuration failed: %w", err)
	}

	if dnsErr != nil && strings.TrimSpace(dnsRaw) == "" {
		return networkSnapshot{}, fmt.Errorf("get dns server list failed: %w", dnsErr)
	}
	dnsInfos, err := decodeJSONList[dnsInfo](dnsRaw)
	if err != nil {
		return networkSnapshot{}, fmt.Errorf("parse dns server list failed: %w", err)
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

func MakeSnapshotEvent() events.Event {
	snap, err := collectNetworkSnapshot()
	if err != nil {
		return events.Event{}
	}
	return events.Event{Type: "network.status", Message: "network snapshot", Data: snap}
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
	dialer := net.Dialer{Timeout: 3 * time.Second}
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

func probeWarpStatus(ctx context.Context) warpSnapshot {
	result := warpSnapshot{CheckedAt: time.Now().Format(time.RFC3339)}
	out, err := execWithTimeout(ctx, "warp-cli", "status")
	result.Raw = strings.TrimSpace(out)
	if err != nil && result.Raw == "" {
		result.Error = err.Error()
		return result
	}
	result.Connected, result.Status = parseWarpConnected(result.Raw)
	return result
}

func probeWarpSettings(ctx context.Context) warpSettingsSnapshot {
	result := warpSettingsSnapshot{CheckedAt: time.Now().Format(time.RFC3339)}
	out, err := execWithTimeout(ctx, "warp-cli", "settings")
	raw := strings.TrimSpace(out)
	if err != nil && raw == "" {
		result.Error = err.Error()
		return result
	}
	result.Mode = parseWarpSettingsValue(raw, "Mode")
	result.TunnelProtocol = parseWarpSettingsValue(raw, "WARP tunnel protocol")
	return result
}

func parseWarpSettingsValue(raw, key string) string {
	needle := key + ":"
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		idx := strings.Index(line, needle)
		if idx < 0 {
			continue
		}
		value := strings.TrimSpace(line[idx+len(needle):])
		if value != "" {
			return value
		}
	}
	return ""
}

func parseWarpConnected(raw string) (bool, string) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return false, ""
	}
	if strings.Contains(text, "status update: connected") && strings.Contains(text, "network: healthy") {
		return true, "Connected"
	}
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" || value == "" {
			continue
		}
		keyLower := strings.ToLower(key)
		if strings.Contains(keyLower, "status") || strings.Contains(keyLower, "state") {
			valueLower := strings.ToLower(value)
			if strings.Contains(valueLower, "connect") || strings.Contains(valueLower, "check") || strings.Contains(valueLower, "update") || strings.Contains(valueLower, "disabled") || strings.Contains(valueLower, "off") || strings.Contains(valueLower, "disconnect") {
				return false, value
			}
			return false, ""
		}
	}
	return false, ""
}



func execWithTimeout(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func notify(hub *events.Hub, typ, msg string, data interface{}) {
	if hub == nil {
		return
	}
	hub.Publish(events.Event{Type: typ, Message: msg, Data: data})
}

func getIPv6AdminState(ifName string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func getAdapterDnsServers(ifName string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	psCmd := fmt.Sprintf("Get-DnsClientServerAddress | Where-Object { $_.InterfaceAlias -eq '%s' } | Select-Object InterfaceAlias, ServerAddresses | ConvertTo-Json -Compress", escapePowerShellSingleQuotedString(ifName))
	raw, err := execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	dnsInfos, err := decodeJSONList[dnsInfo](raw)
	if err != nil {
		return nil, err
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
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	psCmd := fmt.Sprintf("Set-DnsClientServerAddress -InterfaceAlias '%s' -ServerAddresses %s -Confirm:$false -ErrorAction Stop", escapePowerShellSingleQuotedString(ifName), joinPowerShellStringArray(servers))
	return execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psCmd)
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return execWithTimeout(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script.String())
}

func SwitchStackHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			IfName string `json:"ifName"`
			Mode   string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
			notify(hub, "switch.error", "invalid request body", nil)
			return
		}

		ok, _ := isAdmin()
		if !ok {
			writeJSON(w, map[string]string{"error": "admin required"}, http.StatusForbidden)
			notify(hub, "switch.error", "administrator privilege required", payload)
			return
		}

		if payload.Mode != "ipv4" && payload.Mode != "ipv6" && payload.Mode != "both" {
			writeJSON(w, map[string]string{"error": "unknown mode"}, http.StatusBadRequest)
			notify(hub, "switch.error", "unknown mode", payload)
			return
		}

		out, err := applyNetworkMode(payload.IfName, payload.Mode)
		if err != nil {
			result := map[string]interface{}{
				"error":  "command failed",
				"detail": err.Error(),
				"output": out,
			}
			writeJSON(w, result, http.StatusInternalServerError)
			notify(hub, "switch.error", "failed to switch network stack", map[string]interface{}{
				"request": payload,
				"detail":  err.Error(),
				"output":  out,
			})
			return
		}

		result := map[string]interface{}{"ok": true, "output": out}
		writeJSON(w, result, http.StatusOK)
		notify(hub, "switch.ok", "network stack updated", map[string]interface{}{
			"request": payload,
			"output":  strings.TrimSpace(out),
		})
	}
}


func WarpHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
			notify(hub, "warp.error", "invalid request body", nil)
			return
		}

		ok, _ := isAdmin()
		if !ok {
			writeJSON(w, map[string]string{"error": "admin required"}, http.StatusForbidden)
			notify(hub, "warp.error", "administrator privilege required", payload)
			return
		}

		if _, err := exec.LookPath("warp-cli"); err != nil {
			writeJSON(w, map[string]string{"error": "warp-cli not found; please install Cloudflare WARP client"}, http.StatusBadRequest)
			notify(hub, "warp.error", "warp-cli not found", nil)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		out, err := applyWarpAction(ctx, payload.Action)
		if errors.Is(err, errUnknownWarpAction) {
			writeJSON(w, map[string]string{"error": "unknown action"}, http.StatusBadRequest)
			notify(hub, "warp.error", "unknown action", payload)
			return
		}

		if err != nil {
			result := map[string]interface{}{"error": "warp error", "detail": err.Error(), "output": out}
			writeJSON(w, result, http.StatusInternalServerError)
			notify(hub, "warp.error", "failed to update warp state", map[string]interface{}{
				"request": payload,
				"detail":  err.Error(),
				"output":  out,
			})
			return
		}

		warpProbe := probeWarpStatus(ctx)

		result := map[string]interface{}{"ok": true, "output": out, "connected": warpProbe.Connected, "status": warpProbe.Status}
		writeJSON(w, result, http.StatusOK)
		notify(hub, "warp.ok", "warp state updated", map[string]interface{}{
			"request":   payload,
			"output":    strings.TrimSpace(out),
			"connected": warpProbe.Connected,
			"status":    warpProbe.Status,
		})
	}
}

func WarpStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		probe := probeWarpStatus(ctx)
		writeJSON(w, map[string]interface{}{
			"connected": probe.Connected,
			"status":    probe.Status,
		}, http.StatusOK)
	}
}

func DnsHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			IfName      string    `json:"ifName"`
			IPv4Servers *[]string `json:"ipv4Servers"`
			IPv6Servers *[]string `json:"ipv6Servers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
			notify(hub, "dns.error", "invalid request body", nil)
			return
		}

		ok, _ := isAdmin()
		if !ok {
			writeJSON(w, map[string]string{"error": "admin required"}, http.StatusForbidden)
			notify(hub, "dns.error", "administrator privilege required", payload)
			return
		}

		if strings.TrimSpace(payload.IfName) == "" {
			writeJSON(w, map[string]string{"error": "missing ifName"}, http.StatusBadRequest)
			notify(hub, "dns.error", "missing ifName", payload)
			return
		}
		if payload.IPv4Servers == nil && payload.IPv6Servers == nil {
			writeJSON(w, map[string]string{"error": "no dns changes provided"}, http.StatusBadRequest)
			notify(hub, "dns.error", "no dns changes provided", payload)
			return
		}

		out, err := applyDnsServers(payload.IfName, payload.IPv4Servers, payload.IPv6Servers)
		if err != nil {
			writeJSON(w, map[string]interface{}{"error": "command failed", "detail": err.Error(), "output": out}, http.StatusInternalServerError)
			notify(hub, "dns.error", "failed to update dns servers", map[string]interface{}{
				"request": payload,
				"detail":  err.Error(),
				"output":  out,
			})
			return
		}

		writeJSON(w, map[string]interface{}{"ok": true, "output": out}, http.StatusOK)
		notify(hub, "dns.ok", "dns servers updated", map[string]interface{}{
			"request": payload,
			"output":  strings.TrimSpace(out),
		})
	}
}

var errUnknownWarpAction = errors.New("unknown warp action")

func applyWarpAction(ctx context.Context, action string) (string, error) {
	switch action {
	case "start":
		return execWithTimeout(ctx, "warp-cli", "connect")
	case "stop":
		return execWithTimeout(ctx, "warp-cli", "disconnect")
	default:
		return "", errUnknownWarpAction
	}
}

func StartWarp() error {
	if _, err := exec.LookPath("warp-cli"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := applyWarpAction(ctx, "start")
	return err
}

func SettingsHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cfg, err := appsettings.Load()
			if err != nil {
				writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
				return
			}
			writeJSON(w, settingsSnapshot{AutoStart: cfg.AutoStart, SilentStart: cfg.SilentStart, WarpAutoStart: cfg.WarpAutoStart, WarpAppAutoStart: cfg.WarpAppAutoStart}, http.StatusOK)
		case http.MethodPost:
			var payload settingsSnapshot
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSON(w, map[string]string{"error": "invalid json"}, http.StatusBadRequest)
				return
			}
			prevCfg, err := appsettings.Load()
			if err != nil {
				writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
				return
			}
			cfg := appsettings.Settings{AutoStart: payload.AutoStart, SilentStart: payload.SilentStart, WarpAutoStart: payload.WarpAutoStart, WarpAppAutoStart: payload.WarpAppAutoStart}
			if err := appsettings.Save(cfg); err != nil {
				writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
				return
			}
			if prevCfg.AutoStart != cfg.AutoStart {
				if err := appsettings.ApplyStartupShortcut(cfg.AutoStart); err != nil {
					writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
					return
				}
			}
			if prevCfg.WarpAppAutoStart != cfg.WarpAppAutoStart {
				if err := appsettings.ApplyWarpAppStartupShortcut(cfg.WarpAppAutoStart); err != nil {
					writeJSON(w, map[string]string{"error": err.Error()}, http.StatusInternalServerError)
					return
				}
			}
			notify(hub, "settings.ok", "settings updated", settingsSnapshot{AutoStart: cfg.AutoStart, SilentStart: cfg.SilentStart, WarpAutoStart: cfg.WarpAutoStart, WarpAppAutoStart: cfg.WarpAppAutoStart})
			writeJSON(w, map[string]any{"ok": true, "settings": settingsSnapshot{AutoStart: cfg.AutoStart, SilentStart: cfg.SilentStart, WarpAutoStart: cfg.WarpAutoStart, WarpAppAutoStart: cfg.WarpAppAutoStart}}, http.StatusOK)
		default:
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
		}
	}
}

func WSHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("ws upgrade error:", err)
			return
		}
		defer c.Close()

		sub := hub.Subscribe(8)
		defer hub.Unsubscribe(sub)

		_ = c.WriteJSON(events.Event{Type: "hello", Message: "connected to BKNetwork"})
		if snap, err := collectNetworkSnapshot(); err == nil {
			_ = c.WriteJSON(events.Event{Type: "network.status", Message: "network snapshot", Data: snap})
		}

		for {
			event, ok := <-sub
			if !ok {
				return
			}
			if err := c.WriteJSON(event); err != nil {
				log.Println("ws write error:", err)
				return
			}
		}
	}
}

func StatusHandler(hub *events.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		last, hasLast := hub.Snapshot()
		var lastEvent interface{}
		if hasLast {
			lastEvent = last
		}
		admin, adminErr := isAdmin()
		var adminErrMsg string
		if adminErr != nil {
			adminErrMsg = adminErr.Error()
		}
		network, netErr := collectNetworkSnapshot()
		var netErrMsg string
		if netErr != nil {
			netErrMsg = netErr.Error()
		}
		writeJSON(w, map[string]interface{}{
			"service": map[string]interface{}{
				"name":    "BKNetwork",
				"version": "dev",
			},
			"admin":      admin,
			"adminError": adminErrMsg,
			"connection": map[string]interface{}{
				"websocket": "/ws",
			},
			"lastEvent":    lastEvent,
			"network":      network,
			"networkError": netErrMsg,
			"time":         time.Now().Format(time.RFC3339),
		}, http.StatusOK)
	}
}

func LatestVersionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, map[string]string{"error": "method not allowed"}, http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()

		tag, err := fetchLatestReleaseTag(ctx)
		if err != nil {
			log.Printf("latest version check failed: %v", err)
			writeJSON(w, map[string]interface{}{"ok": true, "tag": ""}, http.StatusOK)
			return
		}

		writeJSON(w, map[string]interface{}{"ok": true, "tag": tag}, http.StatusOK)
	}
}

func fetchLatestReleaseTag(ctx context.Context) (string, error) {
	if tag, err := fetchLatestReleaseTagFromRedirect(ctx); err == nil {
		return tag, nil
	}

	return fetchLatestReleaseTagFromAPI(ctx)
}

func fetchLatestReleaseTagFromRedirect(ctx context.Context) (string, error) {
	const latestReleaseURL = "https://github.com/evansrrr/BKNetwork/releases/latest"

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BKNetwork-Version-Checker")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.Request == nil || resp.Request.URL == nil {
		return "", errors.New("missing redirect target url")
	}

	tag := strings.TrimSpace(path.Base(resp.Request.URL.Path))
	if tag == "" || strings.EqualFold(tag, "latest") {
		return "", errors.New("invalid redirect tag")
	}
	return tag, nil
}

func fetchLatestReleaseTagFromAPI(ctx context.Context) (string, error) {
	const releasesAPI = "https://api.github.com/repos/evansrrr/BKNetwork/releases?per_page=100"

	client := &http.Client{Timeout: 8 * time.Second}
	apiReq, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return "", err
	}
	apiReq.Header.Set("Accept", "application/vnd.github+json")
	apiReq.Header.Set("User-Agent", "BKNetwork-Version-Checker")

	resp, err := client.Do(apiReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var payload []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	stableTags := make([]string, 0, len(payload))
	allTags := make([]string, 0, len(payload))
	for _, item := range payload {
		tag := strings.TrimSpace(item.TagName)
		if tag == "" || item.Draft {
			continue
		}
		allTags = append(allTags, tag)
		if !item.Prerelease {
			stableTags = append(stableTags, tag)
		}
	}

	if tag, ok := selectHighestReleaseTag(stableTags); ok {
		return tag, nil
	}
	if tag, ok := selectHighestReleaseTag(allTags); ok {
		return tag, nil
	}

	if len(allTags) > 0 {
		return allTags[0], nil
	}

	if len(payload) == 0 {
		return "", errors.New("empty release list from github")
	}

	first := strings.TrimSpace(payload[0].TagName)
	if first == "" {
		return "", errors.New("empty tag from github")
	}
	return first, nil
}

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func parseSemanticVersion(tag string) (semanticVersion, bool) {
	cleaned := strings.TrimSpace(tag)
	cleaned = strings.TrimPrefix(cleaned, "tag/")
	cleaned = strings.TrimPrefix(cleaned, "v")
	if cleaned == "" {
		return semanticVersion{}, false
	}
	if plus := strings.Index(cleaned, "+"); plus >= 0 {
		cleaned = cleaned[:plus]
	}

	core := cleaned
	pre := ""
	if dash := strings.Index(cleaned, "-"); dash >= 0 {
		core = cleaned[:dash]
		pre = strings.TrimSpace(cleaned[dash+1:])
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semanticVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semanticVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semanticVersion{}, false
	}

	return semanticVersion{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: pre,
	}, true
}

func compareSemanticVersion(left, right semanticVersion) int {
	if left.major != right.major {
		return left.major - right.major
	}
	if left.minor != right.minor {
		return left.minor - right.minor
	}
	if left.patch != right.patch {
		return left.patch - right.patch
	}
	if left.prerelease == right.prerelease {
		return 0
	}
	if left.prerelease == "" {
		return 1
	}
	if right.prerelease == "" {
		return -1
	}
	return strings.Compare(left.prerelease, right.prerelease)
}

func selectHighestReleaseTag(tags []string) (string, bool) {
	bestTag := ""
	var bestVersion semanticVersion
	found := false

	for _, candidate := range tags {
		version, ok := parseSemanticVersion(candidate)
		if !ok {
			continue
		}
		if !found || compareSemanticVersion(version, bestVersion) > 0 {
			bestTag = candidate
			bestVersion = version
			found = true
		}
	}

	return bestTag, found
}

func isAdmin() (bool, error) {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false, err
	}
	defer token.Close()

	var elevation uint32
	var retLen uint32
	err = windows.GetTokenInformation(token, windows.TokenElevation, (*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &retLen)
	if err != nil {
		return false, err
	}
	return elevation != 0, nil
}
