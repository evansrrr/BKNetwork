//go:build darwin

package handlers

import "testing"

func TestParseDarwinNetworkServices(t *testing.T) {
	raw := `An asterisk (*) denotes that a network service is disabled.
(1) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)
(2) *Thunderbolt Bridge
(Hardware Port: Thunderbolt Bridge, Device: bridge0)
`

	services := parseDarwinNetworkServices(raw)
	if len(services) != 2 {
		t.Fatalf("parseDarwinNetworkServices() returned %d services, want 2", len(services))
	}
	if services[0].Name != "Wi-Fi" || services[0].Device != "en0" {
		t.Fatalf("first service = %#v, want Wi-Fi/en0", services[0])
	}
	if services[1].Name != "Thunderbolt Bridge" || services[1].Device != "bridge0" {
		t.Fatalf("second service = %#v, want Thunderbolt Bridge/bridge0", services[1])
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("Kid's Wi-Fi"), `'Kid'"'"'s Wi-Fi'`; got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func TestPreferredDarwinServices(t *testing.T) {
	services := []darwinNetworkService{
		{Name: "Bridge"},
		{Name: "Wi-Fi"},
	}
	adapters := []adapterSnapshot{
		{Name: "Bridge"},
		{Name: "Wi-Fi", IPv4: []string{"192.0.2.2"}},
	}

	got := preferredDarwinServices(services, adapters)
	if len(got) != 2 || got[0] != "Wi-Fi" || got[1] != "Bridge" {
		t.Fatalf("preferredDarwinServices() = %#v, want [Wi-Fi Bridge]", got)
	}
}
