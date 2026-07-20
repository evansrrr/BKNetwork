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
