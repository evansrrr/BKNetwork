package handlers

import (
	"errors"
	"strings"
	"testing"
)

func TestDarwinSwitchSteps(t *testing.T) {
	tests := []struct {
		mode     string
		wantArgs [][]string
		wantUndo [][]string
		wantErr  bool
	}{
		{
			mode:     "ipv4",
			wantArgs: [][]string{{"-setdhcp", "Wi-Fi"}, {"-setv6off", "Wi-Fi"}},
			wantUndo: [][]string{nil, {"-setv6automatic", "Wi-Fi"}},
		},
		{
			mode:     "ipv6",
			wantArgs: [][]string{{"-setv6automatic", "Wi-Fi"}, {"-setv4off", "Wi-Fi"}},
			wantUndo: [][]string{nil, {"-setdhcp", "Wi-Fi"}},
		},
		{
			mode:     "both",
			wantArgs: [][]string{{"-setdhcp", "Wi-Fi"}, {"-setv6automatic", "Wi-Fi"}},
			wantUndo: [][]string{{"-setv4off", "Wi-Fi"}, {"-setv6off", "Wi-Fi"}},
		},
		{mode: "ipv5", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			steps, err := darwinSwitchSteps("/usr/sbin/networksetup", "Wi-Fi", tc.mode)
			if tc.wantErr {
				if !errors.Is(err, errUnknownMode) {
					t.Fatalf("error = %v, want errUnknownMode", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(steps) != len(tc.wantArgs) {
				t.Fatalf("got %d steps, want %d", len(steps), len(tc.wantArgs))
			}
			for i, step := range steps {
				if step.Args[0] != "/usr/sbin/networksetup" {
					t.Fatalf("step %d does not use networksetup: %v", i, step.Args)
				}
				if got := strings.Join(step.Args[1:], " "); got != strings.Join(tc.wantArgs[i], " ") {
					t.Fatalf("step %d args = %q, want %q", i, got, strings.Join(tc.wantArgs[i], " "))
				}
				gotUndo := ""
				if len(step.Undo) > 1 {
					gotUndo = strings.Join(step.Undo[1:], " ")
				}
				wantUndo := ""
				if len(tc.wantUndo[i]) > 0 {
					wantUndo = strings.Join(tc.wantUndo[i], " ")
				}
				if gotUndo != wantUndo {
					t.Fatalf("step %d undo = %q, want %q", i, gotUndo, wantUndo)
				}
			}
		})
	}
}

func TestBuildDarwinShellScriptRollsBackPreviousSteps(t *testing.T) {
	steps, err := darwinSwitchSteps("/usr/sbin/networksetup", "Wi-Fi", "ipv6")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	script := buildDarwinShellScript(steps)

	// Enabling IPv6 is not reversible in a meaningful way, but switching IPv4
	// off is: when it fails, the script must restore DHCP on the service.
	if !strings.Contains(script, "'/usr/sbin/networksetup' '-setv4off' 'Wi-Fi' || { ") {
		t.Fatalf("script does not guard the failing step:\n%s", script)
	}
	if !strings.Contains(script, "echo ") || !strings.Contains(script, "exit 1") {
		t.Fatalf("script does not abort on failure:\n%s", script)
	}
}

func TestBuildDarwinShellScriptRestoresOnSecondFailure(t *testing.T) {
	steps := []darwinSwitchStep{
		{Label: "first", Args: []string{"cmd1"}, Undo: []string{"undo1"}},
		{Label: "second", Args: []string{"cmd2"}, Undo: []string{"undo2"}},
		{Label: "third", Args: []string{"cmd3"}},
	}
	script := buildDarwinShellScript(steps)

	// The third step has no undo of its own, so the failure handler must run
	// the undo of every earlier step, newest first.
	thirdLine := script[strings.Index(script, "'cmd3'"):]
	undo2 := strings.Index(thirdLine, "'undo2'")
	undo1 := strings.Index(thirdLine, "'undo1'")
	if undo2 < 0 || undo1 < 0 {
		t.Fatalf("third step failure does not restore earlier steps:\n%s", script)
	}
	if undo2 > undo1 {
		t.Fatalf("undo commands are not applied newest first:\n%s", script)
	}
}

func TestBuildDarwinShellScriptQuotesServiceName(t *testing.T) {
	steps, err := darwinSwitchSteps("/usr/sbin/networksetup", "Kid's Wi-Fi", "ipv4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	script := buildDarwinShellScript(steps)
	want := `'Kid'"'"'s Wi-Fi'`
	if !strings.Contains(script, want) {
		t.Fatalf("service name was not shell quoted, want %q in:\n%s", want, script)
	}
}

func TestAppleScriptEscape(t *testing.T) {
	if got, want := appleScriptEscape(`a"b\c`), `a\"b\\c`; got != want {
		t.Fatalf("appleScriptEscape() = %q, want %q", got, want)
	}
}
