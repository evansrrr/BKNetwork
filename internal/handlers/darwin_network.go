//go:build darwin

package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const networkSetupPath = "/usr/sbin/networksetup"

func applyDarwinNetworkMode(serviceName, mode string) (string, error) {
	var commands [][]string
	switch mode {
	case "ipv4":
		commands = [][]string{
			{networkSetupPath, "-setdhcp", serviceName},
			{networkSetupPath, "-setv6off", serviceName},
		}
	case "ipv6":
		commands = [][]string{
			{networkSetupPath, "-setv6automatic", serviceName},
			{networkSetupPath, "-setv4off", serviceName},
		}
	case "both":
		commands = [][]string{
			{networkSetupPath, "-setdhcp", serviceName},
			{networkSetupPath, "-setv6automatic", serviceName},
		}
	default:
		return "", fmt.Errorf("unknown mode: %s", mode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := runDarwinAdminCommands(ctx, commands)
	if err == nil {
		time.Sleep(time.Second)
	}
	return out, err
}

func getDarwinIPv6State(serviceName string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()
	raw, err := execWithTimeout(ctx, networkSetupPath, "-getinfo", serviceName)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "IPv6 IP address") {
			continue
		}
		value = strings.TrimSpace(value)
		return value != "" && !strings.EqualFold(value, "none"), nil
	}
	return false, nil
}

func getDarwinAdapterDnsServers(serviceName string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutShort)
	defer cancel()
	raw, err := execWithTimeout(ctx, networkSetupPath, "-getdnsservers", serviceName)
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	if strings.Contains(strings.ToLower(raw), "there aren't any dns servers") {
		return []string{}, nil
	}
	servers := make([]string, 0)
	for _, line := range strings.Split(raw, "\n") {
		if server := strings.TrimSpace(line); server != "" {
			servers = append(servers, server)
		}
	}
	return servers, nil
}

func setDarwinAdapterDnsServers(serviceName string, servers []string) (string, error) {
	args := []string{networkSetupPath, "-setdnsservers", serviceName}
	if len(servers) == 0 {
		args = append(args, "empty")
	} else {
		args = append(args, servers...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := runDarwinAdminCommands(ctx, [][]string{args})
	if err == nil {
		time.Sleep(300 * time.Millisecond)
	}
	return out, err
}

func runDarwinAdminCommands(ctx context.Context, commands [][]string) (string, error) {
	parts := make([]string, 0, len(commands))
	for _, command := range commands {
		quoted := make([]string, 0, len(command))
		for _, arg := range command {
			quoted = append(quoted, shellQuote(arg))
		}
		parts = append(parts, strings.Join(quoted, " "))
	}
	shellCommand := strings.Join(parts, " && ")
	script := fmt.Sprintf("do shell script \"%s\" with administrator privileges", appleScriptEscape(shellCommand))
	return execWithTimeout(ctx, "osascript", "-e", script)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func appleScriptEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\"", "\\\"")
}
