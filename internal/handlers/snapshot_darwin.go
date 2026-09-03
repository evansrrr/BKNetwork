//go:build darwin

package handlers

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

type darwinNetworkService struct {
	Name         string
	HardwarePort string
	Device       string
}

func collectDarwinNetworkSnapshot() (networkSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutMedium)
	defer cancel()
	services, err := listDarwinNetworkServices(ctx)
	if err != nil {
		return networkSnapshot{CollectedAt: time.Now().Format(time.RFC3339)}, err
	}

	var warpStatus warpSnapshot
	var warpSettings warpSettingsSnapshot
	var tcpProbe tcpProbeSnapshot
	var wg sync.WaitGroup
	wg.Add(3)
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

	adapters := make([]adapterSnapshot, 0, len(services))
	for _, service := range services {
		adapter := darwinAdapterSnapshot(ctx, service)
		if adapter.Status == "up" || len(adapter.IPv4) > 0 || len(adapter.IPv6) > 0 {
			adapters = append(adapters, adapter)
		}
	}
	wg.Wait()

	for i := range adapters {
		adapters[i].FreeFlow = warpStatus.Connected && adapters[i].IPv6Enabled && !adapters[i].IPv4Enabled
	}
	available := preferredDarwinServices(services, adapters)
	sort.Slice(adapters, func(i, j int) bool {
		return strings.ToLower(adapters[i].Name) < strings.ToLower(adapters[j].Name)
	})

	return networkSnapshot{
		CollectedAt:       time.Now().Format(time.RFC3339),
		Online:            hasOnlineAdapter(adapters),
		Adapters:          adapters,
		AvailableAdapters: available,
		CloudflareTCP:     tcpProbe,
		Warp:              warpStatus,
		WarpSettings:      warpSettings,
	}, nil
}

func preferredDarwinServices(services []darwinNetworkService, adapters []adapterSnapshot) []string {
	available := make([]string, 0, len(services))
	seen := make(map[string]struct{}, len(services))
	for _, adapter := range adapters {
		if len(adapter.IPv4) == 0 && len(adapter.IPv6) == 0 && adapter.IPv4Gateway == "" && adapter.IPv6Gateway == "" {
			continue
		}
		available = append(available, adapter.Name)
		seen[adapter.Name] = struct{}{}
	}
	for _, service := range services {
		if _, ok := seen[service.Name]; ok {
			continue
		}
		available = append(available, service.Name)
	}
	return available
}

func listDarwinNetworkServices(ctx context.Context) ([]darwinNetworkService, error) {
	raw, err := execWithTimeout(ctx, networkSetupPath, "-listnetworkserviceorder")
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	services := parseDarwinNetworkServices(raw)
	if len(services) == 0 {
		return nil, fmt.Errorf("no macOS network services found")
	}
	return services, nil
}

func parseDarwinNetworkServices(raw string) []darwinNetworkService {
	services := make([]darwinNetworkService, 0)
	currentName := ""
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "(") && !strings.HasPrefix(line, "(Hardware Port:") {
			if end := strings.Index(line, ")"); end >= 0 {
				currentName = strings.TrimSpace(line[end+1:])
				currentName = strings.TrimPrefix(currentName, "*")
			}
			continue
		}
		if currentName == "" || !strings.HasPrefix(line, "(Hardware Port:") {
			continue
		}
		content := strings.TrimSuffix(strings.TrimPrefix(line, "("), ")")
		parts := strings.Split(content, ",")
		if len(parts) != 2 {
			continue
		}
		hardware := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[0]), "Hardware Port:"))
		device := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[1]), "Device:"))
		if currentName != "" && device != "" {
			services = append(services, darwinNetworkService{Name: currentName, HardwarePort: hardware, Device: device})
		}
		currentName = ""
	}
	return services
}

func darwinAdapterSnapshot(ctx context.Context, service darwinNetworkService) adapterSnapshot {
	adapter := adapterSnapshot{
		Name:        service.Name,
		Status:      "down",
		Description: fmt.Sprintf("%s (%s)", service.HardwarePort, service.Device),
		DNS:         []string{},
		IPv4:        []string{},
		IPv6:        []string{},
	}

	if iface, err := net.InterfaceByName(service.Device); err == nil {
		adapter.MacAddress = iface.HardwareAddr.String()
		if iface.Flags&net.FlagUp != 0 {
			adapter.Status = "up"
		}
		if addrs, addrErr := iface.Addrs(); addrErr == nil {
			for _, addr := range addrs {
				ip, _, parseErr := net.ParseCIDR(addr.String())
				if parseErr != nil {
					continue
				}
				if ip.To4() != nil {
					adapter.IPv4 = append(adapter.IPv4, ip.String())
				} else if ip.To16() != nil {
					adapter.IPv6 = append(adapter.IPv6, ip.String())
				}
			}
		}
	}

	if info, err := execWithTimeout(ctx, networkSetupPath, "-getinfo", service.Name); err == nil || info != "" {
		for _, line := range strings.Split(info, "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			switch strings.TrimSpace(key) {
			case "Router":
				if !strings.EqualFold(value, "none") {
					adapter.IPv4Gateway = value
				}
			case "IPv6 Router":
				if !strings.EqualFold(value, "none") {
					adapter.IPv6Gateway = value
				}
			}
		}
	}
	if dns, err := getDarwinAdapterDnsServers(service.Name); err == nil {
		adapter.DNS = dns
	}
	adapter.IPv4Enabled = len(adapter.IPv4) > 0
	adapter.IPv6Enabled = len(adapter.IPv6) > 0
	return adapter
}
