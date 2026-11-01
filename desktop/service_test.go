package main

import (
	"errors"
	"fmt"
	"github.com/RSISA-Semarang/rsisa-print-server/internal/printserver"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestListenErrorAddressInUse(t *testing.T) {
	for _, cause := range []error{syscall.EADDRINUSE, syscall.Errno(10048)} {
		err := listenError(fmt.Errorf("failed to listen: %w", cause), "127.0.0.1:17891")
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "ubah port di Pengaturan") {
			t.Fatalf("galat port dipakai tidak memberi petunjuk: %v", err)
		}
	}
}

func TestServicePortConflictAndRecovery(t *testing.T) {
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	cfg := printserver.DefaultConfig()
	cfg.Listen = occupied.Addr().String()
	c := NewServiceController(func() printserver.Config { return cfg }, func(string, string) {}, nil)
	defer c.Stop()
	if err := c.Start(); err == nil {
		t.Fatal("port yang dipakai harus langsung ditolak")
	}
	if c.State().Status != statusError {
		t.Fatal("status harus error")
	}
	occupied.Close()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for !probeHealth(client, cfg.Listen) {
		if time.Now().After(deadline) {
			t.Fatal("layanan tidak pulih setelah port bebas")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServiceConcurrentRestartReleasesPort(t *testing.T) {
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := printserver.DefaultConfig()
	cfg.Listen = probe.Addr().String()
	probe.Close()
	c := NewServiceController(func() printserver.Config { return cfg }, func(string, string) {}, nil)
	defer c.Stop()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Restart(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	reopened, err := net.Listen("tcp4", cfg.Listen)
	if err != nil {
		t.Fatalf("port masih tertinggal: %v", err)
	}
	reopened.Close()
	if c.State().Status != statusStopped {
		t.Fatal("status harus stopped")
	}
}

func TestProbeHealthRejectsOtherApplication(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"status":"ok","service":"rsisa-print-server"}`, true},
		{`{"status":"ok","service":"other-app"}`, false},
		{`<html>hello</html>`, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
		got := probeHealth(server.Client(), strings.TrimPrefix(server.URL, "http://"))
		server.Close()
		if got != tc.want {
			t.Errorf("probeHealth(%q) = %v", tc.body, got)
		}
	}
}
