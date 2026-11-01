//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/utils"
)

func portOwnerProcesses(port int) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Query the TCP table first, then resolve each owning PID through the
	// standard process cmdlet. Select-Object keeps the response limited to
	// process names and avoids exposing PIDs in the UI/API.
	script := `$ErrorActionPreference = 'Stop'
$connections = @(Get-NetTCPConnection -LocalPort ` + fmt.Sprint(port) + ` -State Listen -ErrorAction SilentlyContinue)
$names = @($connections | ForEach-Object {
  $process = Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue
  if ($process) { $process.ProcessName }
}) | Where-Object { $_ -and $_.Trim() -ne '' } | Sort-Object -Unique
ConvertTo-Json -InputObject @($names) -Compress`
	command := utils.PowerShellCommand(ctx, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	output, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("pemeriksaan pemilik port Windows timeout")
		}
		return nil, fmt.Errorf("tidak dapat memeriksa pemilik port dengan PowerShell: %w", err)
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" || trimmed == "null" {
		return []string{}, nil
	}
	var names []string
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &names); err != nil {
			return nil, fmt.Errorf("respons pemeriksaan port Windows tidak valid: %w", err)
		}
	} else {
		// ConvertTo-Json dapat mengembalikan string langsung saat hanya ada
		// satu proses, bergantung pada versi PowerShell.
		var name string
		if err := json.Unmarshal([]byte(trimmed), &name); err != nil {
			return nil, fmt.Errorf("respons pemeriksaan port Windows tidak valid: %w", err)
		}
		names = []string{name}
	}
	if names == nil {
		names = []string{}
	}
	if len(names) == 0 {
		return []string{}, nil
	}
	unique := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			unique[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}
