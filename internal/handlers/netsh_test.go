package handlers

import (
	"reflect"
	"testing"
)

func TestParseNetshDNSServers(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int
		wantServers []string
	}{
		{
			name: "DHCP DNS",
			input: `Configuration for interface "WiFi"
    DNS servers configured through DHCP:  202.204.52.94
                                          202.204.52.89
    Register with which suffix:           Primary only`,
			expected:    1,
			wantServers: []string{"202.204.52.94", "202.204.52.89"},
		},
		{
			name: "Static DNS",
			input: `Configuration for interface "WiFi"
    Statically Configured DNS Servers:    1.1.1.1
    Register with which suffix:           Primary only`,
			expected:    1,
			wantServers: []string{"1.1.1.1"},
		},
		{
			name: "Mixed DNS",
			input: `Configuration for interface "WiFi"
    DNS servers configured through DHCP:  8.8.8.8
    Statically Configured DNS Servers:    1.1.1.1
    Register with which suffix:           Primary only`,
			expected:    1,
			wantServers: []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			// A Chinese Windows prints a translated header and label; the
			// section used to be detected by the English literal, so every
			// adapter came back with no DNS servers at all.
			name: "localized zh-CN",
			input: `接口 "WiFi" 的配置
    通过 DHCP 配置的 DNS 服务器:          202.204.52.94
                                          202.204.52.89
    注册后缀:                             仅主后缀`,
			expected:    1,
			wantServers: []string{"202.204.52.94", "202.204.52.89"},
		},
		{
			// IPv6 continuation lines contain colons of their own; they must
			// not be sliced into a key/value pair.
			name: "IPv6 continuation lines",
			input: `Configuration for interface "Ethernet"
    Statically Configured DNS Servers:    2606:4700:4700::1111
                                          2606:4700:4700::1001
    Register with which suffix:           Primary only`,
			expected:    1,
			wantServers: []string{"2606:4700:4700::1111", "2606:4700:4700::1001"},
		},
		{
			name: "localized none",
			input: `接口 "WiFi" 的配置
    静态配置的 DNS 服务器:                无
    注册后缀:                             仅主后缀`,
			expected:    1,
			wantServers: []string{},
		},
		{
			name: "multiple sections",
			input: `Configuration for interface "WiFi"
    Statically Configured DNS Servers:    1.1.1.1
Configuration for interface "Ethernet"
    Statically Configured DNS Servers:    8.8.8.8`,
			expected:    2,
			wantServers: []string{"1.1.1.1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := parseNetshDNSServers(tc.input)
			if len(entries) != tc.expected {
				t.Errorf("expected %d entries, got %d", tc.expected, len(entries))
				for i, e := range entries {
					t.Logf("entry %d: name=%q, servers=%v", i, e.Name, e.Servers)
				}
				return
			}
			gotServers := entries[0].Servers
			if gotServers == nil {
				gotServers = []string{}
			}
			if tc.wantServers != nil && !reflect.DeepEqual(gotServers, tc.wantServers) {
				t.Errorf("servers = %v, want %v", gotServers, tc.wantServers)
			}
		})
	}
}

func TestIsWarpAdapterName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"CloudflareWARP", "CloudflareWARP", true},
		{"cloudflarewarp", "cloudflarewarp", true},
		{"WARP", "WARP", true},
		{"WiFi", "WiFi", false},
		{"Ethernet", "Ethernet", false},
		{"以太网", "以太网", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWarpAdapterName(tc.input); got != tc.expected {
				t.Errorf("isWarpAdapterName(%q) = %v, want %v", tc.input, got, tc.expected)
			}
		})
	}
}

func TestParseNetshInterfaceListLocalized(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantState  map[string]bool
		wantKnown  map[string]bool
		wantIndex  map[string]string
		wantUnknow []string
	}{
		{
			name: "english",
			input: `Idx     Met         MTU          State                Name
---  ----------  ----------  ------------  ---------------------------
  1          75  4294967295  connected     Loopback Pseudo-Interface 1
 25          65        1500  disconnected  Bluetooth Network Connection`,
			wantState: map[string]bool{"Loopback Pseudo-Interface 1": true, "Bluetooth Network Connection": false},
			wantKnown: map[string]bool{"Loopback Pseudo-Interface 1": true, "Bluetooth Network Connection": true},
			wantIndex: map[string]string{"1": "Loopback Pseudo-Interface 1", "25": "Bluetooth Network Connection"},
		},
		{
			// Chinese Windows: translated header and state words. Before the
			// translation table every adapter looked disconnected because the
			// column was compared with "connected".
			name: "zh-CN",
			input: `索引     跃点数      MTU           状态            名称
---  ----------  ----------  ------------  ---------------------------
  5          35        1500  已连接         WiFi
 25          65        1500  已断开连接     Bluetooth Network Connection`,
			wantState: map[string]bool{"WiFi": true, "Bluetooth Network Connection": false},
			wantKnown: map[string]bool{"WiFi": true, "Bluetooth Network Connection": true},
			wantIndex: map[string]string{"5": "WiFi", "25": "Bluetooth Network Connection"},
		},
		{
			name: "ja-JP",
			input: `Idx     Met         MTU          状態                 名前
---  ----------  ----------  -------------  ---------------------------
  5          35        1500  接続済み       Wi-Fi
 25          65        1500  未接続         イーサネット`,
			wantState: map[string]bool{"Wi-Fi": true, "イーサネット": false},
			wantKnown: map[string]bool{"Wi-Fi": true, "イーサネット": true},
			wantIndex: map[string]string{"5": "Wi-Fi", "25": "イーサネット"},
		},
		{
			name: "de-DE",
			input: `Idx     Met         MTU          Status               Name
---  ----------  ----------  -------------------  ---------------------------
  5          35        1500  Verbunden              WLAN
 25          65        1500  Nicht verbunden        Ethernet`,
			wantState: map[string]bool{"WLAN": true, "Ethernet": false},
			wantKnown: map[string]bool{"WLAN": true, "Ethernet": true},
			wantIndex: map[string]string{"5": "WLAN", "25": "Ethernet"},
		},
		{
			// An unknown language must be reported as unknown so the snapshot
			// can fall back to the adapter status instead of showing every
			// adapter as disconnected.
			name: "unknown language",
			input: `Idx     Met         MTU          Estado               Nombre
---  ----------  ----------  -------------------  ---------------------------
  5          35        1500  vinculado            Wi-Fi`,
			wantState:  map[string]bool{"Wi-Fi": false},
			wantKnown:  map[string]bool{"Wi-Fi": false},
			wantIndex:  map[string]string{"5": "Wi-Fi"},
			wantUnknow: []string{"vinculado"},
		},
		{
			name: "header only",
			input: `Idx     Met         MTU          State                Name
---  ----------  ----------  ------------  ---------------------------`,
			wantState: map[string]bool{},
			wantKnown: map[string]bool{},
			wantIndex: map[string]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := parseNetshInterfaceList(tc.input)
			if !reflect.DeepEqual(state.Connected, tc.wantState) {
				t.Errorf("Connected = %v, want %v", state.Connected, tc.wantState)
			}
			if !reflect.DeepEqual(state.Known, tc.wantKnown) {
				t.Errorf("Known = %v, want %v", state.Known, tc.wantKnown)
			}
			if !reflect.DeepEqual(state.Index, tc.wantIndex) {
				t.Errorf("Index = %v, want %v", state.Index, tc.wantIndex)
			}
			if !reflect.DeepEqual(state.UnknownWords, tc.wantUnknow) {
				t.Errorf("UnknownWords = %v, want %v", state.UnknownWords, tc.wantUnknow)
			}
		})
	}
}

func TestNetshStateConnected(t *testing.T) {
	tests := []struct {
		word      string
		connected bool
		known     bool
	}{
		{"connected", true, true},
		{"Connected", true, true},
		{"disconnected", false, true},
		{"connected (site-local)", true, true},
		{"已连接", true, true},
		{"已断开连接", false, true},
		{"verbunden", true, true},
		{"nicht verbunden", false, true},
		{"", false, false},
		{"vinculado", false, false},
	}
	for _, tc := range tests {
		connected, known := netshStateConnected(tc.word)
		if connected != tc.connected || known != tc.known {
			t.Errorf("netshStateConnected(%q) = (%v, %v), want (%v, %v)",
				tc.word, connected, known, tc.connected, tc.known)
		}
	}
}

func TestParseNetshDefaultRoute(t *testing.T) {
	ifaceMap := map[string]string{
		"12": "WiFi",
		"23": "CloudflareWARP",
		"8":  "Ethernet",
		"5":  "Wi-Fi",
	}

	tests := []struct {
		name        string
		prefix      string
		input       string
		ifaceMap    map[string]string
		wantIface   string
		wantGateway string
	}{
		{
			name:   "single physical route",
			prefix: "0.0.0.0/0",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  0       0.0.0.0/0     12   192.168.1.1       WiFi
No     Auto  256     192.168.1.0/24 12  0.0.0.0           WiFi`,
			wantIface:   "WiFi",
			wantGateway: "192.168.1.1",
		},
		{
			name:   "warp listed first but physical should win",
			prefix: "0.0.0.0/0",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  10      0.0.0.0/0     23   172.17.0.2         CloudflareWARP
No     Auto  25      0.0.0.0/0     12   192.168.1.1       WiFi`,
			wantIface:   "WiFi",
			wantGateway: "192.168.1.1",
		},
		{
			name:   "warp with lower metric still loses to physical",
			prefix: "0.0.0.0/0",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  5       0.0.0.0/0     23   172.17.0.2         CloudflareWARP
No     Auto  25      0.0.0.0/0     12   192.168.1.1       WiFi`,
			wantIface:   "WiFi",
			wantGateway: "192.168.1.1",
		},
		{
			name:   "only warp route exists",
			prefix: "0.0.0.0/0",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  10      0.0.0.0/0     23   172.17.0.2         CloudflareWARP`,
			wantIface:   "CloudflareWARP",
			wantGateway: "172.17.0.2",
		},
		{
			name:   "two physical routes pick lower metric",
			prefix: "0.0.0.0/0",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  50      0.0.0.0/0     8    10.0.0.1           Ethernet
No     Auto  25      0.0.0.0/0     12   192.168.1.1       WiFi`,
			wantIface:   "WiFi",
			wantGateway: "192.168.1.1",
		},
		{
			name:   "no default route",
			prefix: "0.0.0.0/0",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  256     192.168.1.0/24 12  0.0.0.0           WiFi`,
			wantIface: "",
		},
		{
			name:   "ipv6 physical preferred over warp",
			prefix: "::/0",
			input: `Publish Type  Metric  Prefix                  Idx  Gateway           Interface
------------------------------------------------------------------------------
No     Auto  10      ::/0                    23   fe80::1           CloudflareWARP
No     Auto  25      ::/0                    12   fe80::2           WiFi`,
			wantIface:   "WiFi",
			wantGateway: "fe80::2",
		},
		{
			name:   "localized header and publish column",
			prefix: "0.0.0.0/0",
			input: `发布     类型      跃点  前缀                     索引  网关/接口名称
-------  --------  ---  ------------------------  ---  ------------------------
否       手动      9256  0.0.0.0/0                  13  26.0.0.1
否       手动      0    0.0.0.0/0                   5  10.38.0.1`,
			ifaceMap:    map[string]string{"13": "Radmin VPN", "5": "Wi-Fi"},
			wantIface:   "Wi-Fi",
			wantGateway: "10.38.0.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapping := tc.ifaceMap
			if mapping == nil {
				mapping = ifaceMap
			}
			got := parseNetshDefaultRoute(tc.input, tc.prefix, mapping)
			if got.Interface != tc.wantIface {
				t.Errorf("Interface = %q, want %q", got.Interface, tc.wantIface)
			}
			if got.Gateway != tc.wantGateway {
				t.Errorf("Gateway = %q, want %q", got.Gateway, tc.wantGateway)
			}
		})
	}
}
