//go:build !windows

package printserver

import (
	"context"
	"os/exec"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/utils"
)

func hiddenWindowsCommand(ctx context.Context, script string) *exec.Cmd {
	return utils.PowerShellCommand(ctx, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}
