package handlers

import (
	"fmt"
	"strings"
)

// darwinSwitchStep is one networksetup invocation plus the command that undoes
// it when a later step fails.
type darwinSwitchStep struct {
	Label string
	Args  []string
	Undo  []string
}

// darwinSwitchSteps lists the commands for a mode. Undo describes how to
// restore the state of that step, and is used when a later step fails.
func darwinSwitchSteps(networksetup, serviceName, mode string) ([]darwinSwitchStep, error) {
	switch mode {
	case "ipv4":
		return []darwinSwitchStep{
			{
				Label: "enable ipv4 dhcp",
				Args:  []string{networksetup, "-setdhcp", serviceName},
			},
			{
				Label: "disable ipv6",
				Args:  []string{networksetup, "-setv6off", serviceName},
				Undo:  []string{networksetup, "-setv6automatic", serviceName},
			},
		}, nil
	case "ipv6":
		return []darwinSwitchStep{
			{
				Label: "enable ipv6 automatic",
				Args:  []string{networksetup, "-setv6automatic", serviceName},
			},
			{
				Label: "disable ipv4",
				Args:  []string{networksetup, "-setv4off", serviceName},
				Undo:  []string{networksetup, "-setdhcp", serviceName},
			},
		}, nil
	case "both":
		return []darwinSwitchStep{
			{
				Label: "enable ipv4 dhcp",
				Args:  []string{networksetup, "-setdhcp", serviceName},
				Undo:  []string{networksetup, "-setv4off", serviceName},
			},
			{
				Label: "enable ipv6 automatic",
				Args:  []string{networksetup, "-setv6automatic", serviceName},
				Undo:  []string{networksetup, "-setv6off", serviceName},
			},
		}, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownMode, mode)
	}
}

// buildDarwinShellScript chains the steps so that a failure restores every step
// that already ran. The result is handed to `do shell script ... with
// administrator privileges`, so it has to stay a single POSIX sh command list.
func buildDarwinShellScript(steps []darwinSwitchStep) string {
	var b strings.Builder
	applied := make([]string, 0, len(steps))

	for _, step := range steps {
		if len(step.Args) == 0 {
			continue
		}
		b.WriteString(shellQuoteArgs(step.Args) + " || { ")
		for i := len(applied) - 1; i >= 0; i-- {
			b.WriteString(applied[i] + " >/dev/null 2>&1 || true; ")
		}
		b.WriteString("echo " + shellQuote(step.Label+": failed, previous configuration restored") + " >&2; exit 1; }\n")
		if len(step.Undo) > 0 {
			applied = append(applied, shellQuoteArgs(step.Undo))
		}
	}

	return b.String()
}

func shellQuoteArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

// shellQuote single-quotes a value for POSIX sh, escaping embedded quotes.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// appleScriptEscape escapes a string for use inside an AppleScript string
// literal.
func appleScriptEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\"", "\\\"")
}
