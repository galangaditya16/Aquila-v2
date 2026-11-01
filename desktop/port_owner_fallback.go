//go:build !darwin && !windows

package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

func portOwnerProcesses(port int) ([]string, error) {
	address := net.JoinHostPort("127.0.0.1", fmt.Sprint(port))
	listener, err := net.Listen("tcp4", address)
	if err == nil {
		_ = listener.Close()
		return []string{}, nil
	}
	// Fallback platforms can establish that a port is occupied, but cannot
	// reliably identify its owner without an OS-specific process API.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, dialErr := (&net.Dialer{}).DialContext(ctx, "tcp4", address); dialErr == nil {
		_ = conn.Close()
		return []string{"proses lain (nama tidak tersedia pada OS ini)"}, nil
	}
	return []string{"proses lain (nama tidak tersedia pada OS ini)"}, nil
}
