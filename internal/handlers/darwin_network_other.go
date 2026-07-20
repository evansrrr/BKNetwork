//go:build !darwin

package handlers

import "errors"

var errDarwinOnly = errors.New("macOS network operation is unavailable on this platform")

func applyDarwinNetworkMode(string, string) (string, error) {
	return "", errDarwinOnly
}

func getDarwinIPv6State(string) (bool, error) {
	return false, errDarwinOnly
}

func getDarwinAdapterDnsServers(string) ([]string, error) {
	return nil, errDarwinOnly
}

func setDarwinAdapterDnsServers(string, []string) (string, error) {
	return "", errDarwinOnly
}

func collectDarwinNetworkSnapshot() (networkSnapshot, error) {
	return networkSnapshot{}, errDarwinOnly
}
