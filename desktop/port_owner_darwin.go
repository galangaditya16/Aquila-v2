//go:build darwin

package main

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

func portOwnerProcesses(port int) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// -Fc emits command names only (alongside PID records). We discard PID
	// records so the API never exposes them.
	command := exec.CommandContext(ctx, "lsof", "-nP", "-Fc", "-iTCP:"+fmt.Sprint(port), "-sTCP:LISTEN")
	output, err := command.Output()
	if err != nil && len(output) == 0 {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("pemeriksaan pemilik port macOS timeout")
		}
		// lsof exits 1 when no matching socket exists.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []string{}, nil
		}
		return nil, fmt.Errorf("tidak dapat memeriksa pemilik port dengan lsof: %w", err)
	}

	seen := make(map[string]struct{})
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, "c") || len(line) == 1 {
			continue
		}
		name := strings.TrimSpace(line[1:])
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	processes := make([]string, 0, len(seen))
	for name := range seen {
		processes = append(processes, name)
	}
	sort.Strings(processes)
	return processes, nil
}
