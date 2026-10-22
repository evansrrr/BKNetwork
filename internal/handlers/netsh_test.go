package handlers

import (
	"testing"
)

func TestParseNetshDNSServers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name: "DHCP DNS",
			input: `Configuration for interface "WiFi"
    DNS servers configured through DHCP:  202.204.52.94
                                          202.204.52.89
    Register with which suffix:           Primary only`,
			expected: 1,
		},
		{
			name: "Static DNS",
			input: `Configuration for interface "WiFi"
    Statically Configured DNS Servers:    1.1.1.1
    Register with which suffix:           Primary only`,
			expected: 1,
		},
		{
			name: "Mixed DNS",
			input: `Configuration for interface "WiFi"
    DNS servers configured through DHCP:  8.8.8.8
    Statically Configured DNS Servers:    1.1.1.1
    Register with which suffix:           Primary only`,
			expected: 1,
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
			if len(entries) > 0 && len(entries[0].Servers) == 0 {
				t.Error("expected servers to be non-empty")
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

func TestParseNetshIPv4DefaultRouteWithIdx(t *testing.T) {
	ifaceMap := map[string]string{
		"12": "WiFi",
		"23": "CloudflareWARP",
		"8":  "Ethernet",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "single physical route",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  0       0.0.0.0/0     12   192.168.1.1       WiFi
No     Auto  256     192.168.1.0/24 12  0.0.0.0           WiFi`,
			expected: "WiFi",
		},
		{
			name: "warp listed first but physical should win",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  10      0.0.0.0/0     23   172.17.0.2         CloudflareWARP
No     Auto  25      0.0.0.0/0     12   192.168.1.1       WiFi`,
			expected: "WiFi",
		},
		{
			name: "warp with lower metric still loses to physical",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  5       0.0.0.0/0     23   172.17.0.2         CloudflareWARP
No     Auto  25      0.0.0.0/0     12   192.168.1.1       WiFi`,
			expected: "WiFi",
		},
		{
			name: "only warp route exists",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  10      0.0.0.0/0     23   172.17.0.2         CloudflareWARP`,
			expected: "CloudflareWARP",
		},
		{
			name: "two physical routes pick lower metric",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  50      0.0.0.0/0     8    10.0.0.1           Ethernet
No     Auto  25      0.0.0.0/0     12   192.168.1.1       WiFi`,
			expected: "WiFi",
		},
		{
			name: "no default route",
			input: `Publish Type  Metric  Prefix        Idx  Gateway            Interface
------------------------------------------------------------------------------
No     Auto  256     192.168.1.0/24 12  0.0.0.0           WiFi`,
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseNetshIPv4DefaultRouteWithIdx(tc.input, ifaceMap)
			if got != tc.expected {
				t.Errorf("parseNetshIPv4DefaultRouteWithIdx() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestParseNetshIPv6DefaultRouteWithIdx(t *testing.T) {
	ifaceMap := map[string]string{
		"12": "WiFi",
		"23": "CloudflareWARP",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "physical preferred over warp",
			input: `Publish Type  Metric  Prefix                  Idx  Gateway           Interface
------------------------------------------------------------------------------
No     Auto  10      ::/0                    23   fe80::1           CloudflareWARP
No     Auto  25      ::/0                    12   fe80::1           WiFi`,
			expected: "WiFi",
		},
		{
			name: "only warp",
			input: `Publish Type  Metric  Prefix                  Idx  Gateway           Interface
------------------------------------------------------------------------------
No     Auto  10      ::/0                    23   fe80::1           CloudflareWARP`,
			expected: "CloudflareWARP",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseNetshIPv6DefaultRouteWithIdx(tc.input, ifaceMap)
			if got != tc.expected {
				t.Errorf("parseNetshIPv6DefaultRouteWithIdx() = %q, want %q", got, tc.expected)
			}
		})
	}
}
