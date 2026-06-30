//go:build windows

package settings

import (
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
)

func applyStartupTask(taskName, executablePath, arguments string, enabled bool) error {
	if enabled {
		return registerStartupTask(taskName, executablePath, arguments)
	}
	return unregisterStartupTask(taskName)
}

func registerStartupTask(taskName, executablePath, arguments string) error {
	if strings.TrimSpace(taskName) == "" {
		return fmt.Errorf("empty startup task name")
	}
	if strings.TrimSpace(executablePath) == "" {
		return fmt.Errorf("empty startup executable path")
	}
	currentUser, err := user.Current()
	if err != nil {
		return err
	}
	principalUserId := strings.TrimSpace(currentUser.Uid)
	if principalUserId == "" || !strings.HasPrefix(principalUserId, "S-1-") {
		principalUserId = currentUser.Username
	}
	if strings.TrimSpace(principalUserId) == "" {
		principalUserId = currentUser.Name
	}
	if strings.TrimSpace(principalUserId) == "" {
		return fmt.Errorf("empty startup user id")
	}
	triggerUser := strings.TrimSpace(currentUser.Username)
	if triggerUser == "" {
		triggerUser = strings.TrimSpace(currentUser.Name)
	}
	workingDir := filepath.Dir(executablePath)
	if strings.TrimSpace(workingDir) == "" {
		workingDir = "."
	}

	triggerClause := "-AtLogOn"
	if triggerUser != "" {
		triggerClause = fmt.Sprintf("-AtLogOn -User %s", psQuote(triggerUser))
	}
	// Register the scheduled task to run with highest privileges so the
	// application has administrator rights at startup for internal PowerShell
	// operations that require elevation.
	script := fmt.Sprintf(
		"$action = New-ScheduledTaskAction -Execute %s -Argument %s -WorkingDirectory %s; $trigger = New-ScheduledTaskTrigger %s; $principal = New-ScheduledTaskPrincipal -UserId %s -LogonType Interactive -RunLevel Highest; $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable; Register-ScheduledTask -TaskName %s -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null",
		psQuote(executablePath),
		psQuote(arguments),
		psQuote(workingDir),
		triggerClause,
		psQuote(principalUserId),
		psQuote(taskName),
	)
	cmd := newHiddenPowerShellCommand(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("register scheduled task failed: %w: %s", err, trimmed)
		}
		return fmt.Errorf("register scheduled task failed: %w", err)
	}
	return nil
}

func unregisterStartupTask(taskName string) error {
	if strings.TrimSpace(taskName) == "" {
		return fmt.Errorf("empty startup task name")
	}
	script := fmt.Sprintf("$task = Get-ScheduledTask -TaskName %s -ErrorAction SilentlyContinue; if ($null -ne $task) { Unregister-ScheduledTask -TaskName %s -Confirm:$false -ErrorAction Stop | Out-Null }", psQuote(taskName), psQuote(taskName))
	cmd := newHiddenPowerShellCommand(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("unregister scheduled task failed: %w: %s", err, trimmed)
		}
		return fmt.Errorf("unregister scheduled task failed: %w", err)
	}
	return nil
}

func psQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func newHiddenPowerShellCommand(script string) *exec.Cmd {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
