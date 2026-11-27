package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	// errAdapterWildcard rejects a name PowerShell would treat as a pattern.
	// The UI only ever sends an exact adapter name.
	errAdapterWildcard = errors.New("adapter name contains wildcard characters")
	// errAdapterVirtual marks an adapter that cannot carry the campus uplink.
	errAdapterVirtual = errors.New("virtual adapter cannot be used")
	// errAdapterNotUp is returned when the target exists but the link is down.
	errAdapterNotUp = errors.New("adapter is not connected")
)

// adapterWildcardChars are the characters Get-NetAdapter -Name and
// Set-DnsClientServerAddress -InterfaceAlias treat as pattern syntax. A real
// adapter name never needs them.
const adapterWildcardChars = "*?[]"

// virtualAdapterMarkers are name/description fragments of adapters that never
// carry the campus uplink. Physical USB/PCIe adapters do not match these, and
// matching is case-insensitive. Keep this list in sync with fetchAdapterMetadata
// (InterfaceDescription) when new tunnel vendors appear.
var virtualAdapterMarkers = []string{
	"virtual", "vmware", "hyper-v", "vethernet", "wsl", "docker",
	"tap-win32", "tap-", "wintun", "wireguard", "tailscale", "zerotier",
	"radmin", "openvpn", "bluetooth", "wi-fi direct", "npcap loopback",
	"pseudo-interface", "teredo", "isatap", "6to4",
}

// normalizeAdapterName trims the name and rejects the two inputs that would
// either match every adapter or escape the adapter list entirely.
func normalizeAdapterName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("%w: empty name", errAdapterNotFound)
	}
	if strings.ContainsAny(name, adapterWildcardChars) {
		return "", fmt.Errorf("%w: %q", errAdapterWildcard, name)
	}
	if isLoopbackAdapterName(name) {
		return "", fmt.Errorf("%w: %s", errAdapterNotFound, name)
	}
	return name, nil
}

// isVirtualAdapter reports whether an adapter is a VM/tunnel/virtual radio.
func isVirtualAdapter(name, description string) bool {
	haystack := strings.ToLower(name + " " + description)
	for _, marker := range virtualAdapterMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

// matchAdapterTarget applies the target rules to an already fetched adapter
// list. It is split from the platform lookup so it can be tested without a
// network stack.
func matchAdapterTarget(metas []adapterMeta, name string, allowVirtual bool) error {
	for _, meta := range metas {
		if !strings.EqualFold(strings.TrimSpace(meta.Name), name) {
			continue
		}
		if !allowVirtual && isVirtualAdapter(meta.Name, meta.Description) {
			return fmt.Errorf("%w: %s", errAdapterVirtual, name)
		}
		return nil
	}
	return fmt.Errorf("%w: %s", errAdapterNotFound, name)
}

// adapterTargetCheck is wired per platform by an init function. nil means the
// platform has no extra check (the shell command reports unknown names).
var adapterTargetCheck func(ctx context.Context, name string, allowVirtual bool) error

// validateAdapterTarget validates the name the UI sent before any system
// command runs. allowVirtual is true for the "restore to both" path: undoing a
// previous switch must stay possible even when the adapter is now classified
// differently (for example WARP or a VPN tunnel).
func validateAdapterTarget(ctx context.Context, raw string, allowVirtual bool) (string, error) {
	name, err := normalizeAdapterName(raw)
	if err != nil {
		return "", err
	}
	if check := adapterTargetCheck; check != nil {
		if err := check(ctx, name, allowVirtual); err != nil {
			return "", err
		}
	}
	return name, nil
}

// adapterValidationStatus maps a validation error to the HTTP answer. Bad
// input is 400; an existing but unusable adapter is 409 so the UI can tell the
// user to pick a different one instead of retrying.
func adapterValidationStatus(err error) (int, string) {
	switch {
	case errors.Is(err, errAdapterWildcard):
		return http.StatusBadRequest, "invalid-adapter-name"
	case errors.Is(err, errAdapterNotFound):
		return http.StatusBadRequest, "adapter-not-found"
	case errors.Is(err, errAdapterVirtual):
		return http.StatusConflict, "virtual-adapter"
	case errors.Is(err, errAdapterNotUp):
		return http.StatusConflict, "adapter-not-connected"
	default:
		return http.StatusInternalServerError, "adapter-check-failed"
	}
}
