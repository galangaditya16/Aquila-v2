//go:build windows

package utils

import (
	"context"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// PowerShellCommand starts a PowerShell process without opening a console window.
func PowerShellCommand(ctx context.Context, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "powershell.exe", args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	return command
}
