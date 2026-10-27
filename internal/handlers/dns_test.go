package handlers

import (
	"reflect"
	"testing"
)

func TestParseNetshDNSServersKeepsContinuationLines(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "static servers on continuation lines",
			raw: `Configuration for interface "WiFi"
    Statically Configured DNS Servers:    202.204.52.94
                                          202.204.52.89
    Register with which suffix:           Primary only
`,
			want: []string{"202.204.52.94", "202.204.52.89"},
		},
		{
			name: "dhcp servers on continuation lines",
			raw: "Configuration for interface \"WiFi\"\r\n" +
				"    DNS servers configured through DHCP:  8.8.8.8\r\n" +
				"                                          1.1.1.1\r\n" +
				"                                          9.9.9.9\r\n" +
				"    Register with which suffix:           Primary only\r\n",
			want: []string{"8.8.8.8", "1.1.1.1", "9.9.9.9"},
		},
		{
			name: "ipv6 servers",
			raw: `Configuration for interface "WiFi"
    Statically Configured DNS Servers:    2606:4700:4700::1111
                                          2620:fe::fe
    Register with which suffix:           Primary only
`,
			want: []string{"2606:4700:4700::1111", "2620:fe::fe"},
		},
		{
			name: "no servers configured",
			raw: `Configuration for interface "WiFi"
    Statically Configured DNS Servers:    None
    Register with which suffix:           Primary only
`,
			want: nil,
		},
		{
			name: "non address continuation lines are ignored",
			raw: `Configuration for interface "WiFi"
    Statically Configured DNS Servers:    1.1.1.1
                                          not-an-address
    Register with which suffix:           Primary only
`,
			want: []string{"1.1.1.1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := parseNetshDNSServers(tc.raw)
			if len(entries) != 1 {
				t.Fatalf("got %d entries, want 1: %+v", len(entries), entries)
			}
			if !reflect.DeepEqual(entries[0].Servers, tc.want) {
				t.Fatalf("servers = %v, want %v", entries[0].Servers, tc.want)
			}
		})
	}
}

func TestParseNetshDNSServersMultipleInterfaces(t *testing.T) {
	raw := "\ufeffConfiguration for interface \"WiFi\"\n" +
		"    Statically Configured DNS Servers:    1.1.1.1\n" +
		"                                          1.0.0.1\n" +
		"\n" +
		"Configuration for interface \"Ethernet\"\n" +
		"    Statically Configured DNS Servers:    2606:4700:4700::1111\n" +
		"    Register with which suffix:           None\n"

	entries := parseNetshDNSServers(raw)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Name != "WiFi" || !reflect.DeepEqual(entries[0].Servers, []string{"1.1.1.1", "1.0.0.1"}) {
		t.Fatalf("first entry = %+v", entries[0])
	}
	if entries[1].Name != "Ethernet" || !reflect.DeepEqual(entries[1].Servers, []string{"2606:4700:4700::1111"}) {
		t.Fatalf("second entry = %+v", entries[1])
	}
}

func TestPlanDNSServersPreservesUntouchedFamily(t *testing.T) {
	currentIPv4 := []string{"202.204.52.94", "202.204.52.89"}
	currentIPv6 := []string{"2001:da8:200::1"}

	t.Run("only ipv6 requested keeps all ipv4 servers", func(t *testing.T) {
		ipv6 := []string{"2606:4700:4700::1111"}
		got, err := planDNSServers(currentIPv4, currentIPv6, nil, &ipv6)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"202.204.52.94", "202.204.52.89", "2606:4700:4700::1111"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("planDNSServers() = %v, want %v", got, want)
		}
	})

	t.Run("only ipv4 requested keeps ipv6 servers", func(t *testing.T) {
		ipv4 := []string{"1.1.1.1"}
		got, err := planDNSServers(currentIPv4, currentIPv6, &ipv4, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"1.1.1.1", "2001:da8:200::1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("planDNSServers() = %v, want %v", got, want)
		}
	})

	t.Run("both families requested replaces both", func(t *testing.T) {
		ipv4 := []string{"1.1.1.1", "1.0.0.1"}
		ipv6 := []string{"::1"}
		got, err := planDNSServers(currentIPv4, currentIPv6, &ipv4, &ipv6)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"1.1.1.1", "1.0.0.1", "::1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("planDNSServers() = %v, want %v", got, want)
		}
	})

	t.Run("empty request clears a family", func(t *testing.T) {
		empty := []string{}
		got, err := planDNSServers(currentIPv4, currentIPv6, &empty, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"2001:da8:200::1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("planDNSServers() = %v, want %v", got, want)
		}
	})

	t.Run("invalid address is rejected", func(t *testing.T) {
		bad := []string{"not-an-ip"}
		if _, err := planDNSServers(currentIPv4, currentIPv6, &bad, nil); err == nil {
			t.Fatal("expected error for invalid dns server")
		}
	})

	t.Run("no changes keeps current configuration", func(t *testing.T) {
		got, err := planDNSServers(currentIPv4, currentIPv6, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"202.204.52.94", "202.204.52.89", "2001:da8:200::1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("planDNSServers() = %v, want %v", got, want)
		}
	})
}

func TestIsDNSServerToken(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"1.1.1.1", true},
		{"2606:4700:4700::1111", true},
		{"fe80::1%12", true},
		{"[::1]", true},
		{"None", false},
		{"", false},
		{"Primary only", false},
	}
	for _, tc := range tests {
		if got := isDNSServerToken(tc.value); got != tc.want {
			t.Errorf("isDNSServerToken(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
