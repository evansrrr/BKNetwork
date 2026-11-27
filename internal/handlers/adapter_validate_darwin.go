//go:build darwin

package handlers

import (
	"context"
	"fmt"
	"strings"
)

func init() {
	adapterTargetCheck = checkAdapterTargetDarwin
}

// checkAdapterTargetDarwin confirms the service name networksetup will receive
// is one of the configured services. There is no virtual-adapter equivalent to
// filter here: networksetup only exposes real services plus Thunderbolt
// bridges, and the UI lists them with their IP addresses.
func checkAdapterTargetDarwin(ctx context.Context, name string, _ bool) error {
	services, err := listDarwinNetworkServices(ctx)
	if err != nil || len(services) == 0 {
		return nil
	}
	for _, service := range services {
		if strings.EqualFold(strings.TrimSpace(service.Name), name) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", errAdapterNotFound, name)
}
