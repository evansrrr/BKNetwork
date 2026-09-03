//go:build !windows

package handlers

import "os/exec"

func hideCommandWindow(_ *exec.Cmd) {}
