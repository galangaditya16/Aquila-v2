//go:build windows

package utils

import (
	"context"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPowerShellCommandHidesConsole(t *testing.T) {
	command := PowerShellCommand(context.Background(), "-Command", "exit 0")
	settings := command.SysProcAttr
	if settings == nil || !settings.HideWindow || settings.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("PowerShell console not hidden: %+v", command.SysProcAttr)
	}
}
