package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// PortOwnerResult sengaja hanya membawa nama proses. PID dan detail proses
// lain tidak perlu ditampilkan kepada pengguna untuk mendiagnosis konflik
// port, dan dapat berbeda hak akses antar-OS.
type PortOwnerResult struct {
	Port      int      `json:"port"`
	InUse     bool     `json:"in_use"`
	Processes []string `json:"processes"`
	Message   string   `json:"message"`
}

// DiagnosePort memeriksa port TCP lokal yang dipakai untuk print server.
// Pemeriksaan dilakukan oleh desktop app karena browser tidak dapat membaca
// proses OS. Implementasi per OS berada di port_owner_darwin.go dan
// port_owner_windows.go; OS lain memakai fallback net.Dial.
func DiagnosePort(listen string) (PortOwnerResult, error) {
	_, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return PortOwnerResult{}, fmt.Errorf("alamat layanan tidak valid: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return PortOwnerResult{}, fmt.Errorf("port layanan tidak valid: %q", portText)
	}

	processes, err := portOwnerProcesses(port)
	if err != nil {
		return PortOwnerResult{}, err
	}
	result := PortOwnerResult{Port: port, Processes: processes, InUse: len(processes) > 0}
	switch {
	case len(processes) == 0:
		result.Message = fmt.Sprintf("Port %d sedang kosong.", port)
	case len(processes) == 1:
		result.Message = fmt.Sprintf("Port %d dipakai proses %s.", port, processes[0])
	default:
		result.Message = fmt.Sprintf("Port %d dipakai oleh %s.", port, strings.Join(processes, ", "))
	}
	return result, nil
}
