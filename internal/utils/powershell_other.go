//go:build !windows

package utils

import (
	"context"
	"os/exec"
)

func PowerShellCommand(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "powershell.exe", args...)
}
