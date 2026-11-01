package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/printserver"
)

const (
	statusStopped  = "stopped"
	statusStarting = "starting"
	statusRunning  = "running"
	statusError    = "error"

	healthPollInterval = 2 * time.Second
	shutdownTimeout    = 10 * time.Second
)

// ServiceState adalah keadaan layanan yang dikirim ke UI dan tray.
type ServiceState struct {
	Status        string     `json:"status"`
	Listen        string     `json:"listen"`
	HealthOK      bool       `json:"health_ok"`
	UptimeSeconds int64      `json:"uptime_seconds"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// HealthResult adalah hasil tes koneksi ke print server lokal.
type HealthResult struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latency_ms"`
}

// ServiceController mengelola siklus hidup print server: start, stop, dan
// pemantauan kesehatan. Seluruh goroutine pemantau berhenti saat layanan
// distop sehingga tidak ada kebocoran memori atau goroutine.
type ServiceController struct {
	opMu       sync.Mutex
	mu         sync.Mutex
	status     string
	server     *printserver.Server
	startedAt  time.Time
	lastErr    string
	healthOK   bool
	cancel     context.CancelFunc
	done       chan struct{}
	healthDone chan struct{}
	listener   net.Listener

	cfgProvider func() printserver.Config
	logf        printserver.EventLog
	onState     func(ServiceState)
}

func NewServiceController(cfgProvider func() printserver.Config, logf printserver.EventLog, onState func(ServiceState)) *ServiceController {
	return &ServiceController{
		status:      statusStopped,
		cfgProvider: cfgProvider,
		logf:        logf,
		onState:     onState,
	}
}

// State mengembalikan salinan keadaan layanan saat ini.
func (c *ServiceController) State() ServiceState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateLocked()
}

func (c *ServiceController) stateLocked() ServiceState {
	state := ServiceState{
		Status:    c.status,
		HealthOK:  c.healthOK,
		LastError: c.lastErr,
	}
	if c.server != nil {
		state.Listen = c.server.Config().Listen
	}
	if c.status == statusRunning && !c.startedAt.IsZero() {
		started := c.startedAt
		state.StartedAt = &started
		state.UptimeSeconds = int64(time.Since(c.startedAt).Seconds())
	}
	return state
}

func (c *ServiceController) notify() {
	if c.onState == nil {
		return
	}
	c.onState(c.State())
}

// Start menjalankan layanan. Bila sudah berjalan, tidak ada yang terjadi.
func (c *ServiceController) Start() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.start()
}

func (c *ServiceController) start() error {
	if c.State().Status == statusError {
		if err := c.stop(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	if c.status == statusStarting || c.status == statusRunning {
		c.mu.Unlock()
		return nil
	}
	cfg := c.cfgProvider()
	server := printserver.New(cfg, c.logf)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	healthDone := make(chan struct{})
	c.server = server
	c.cancel = cancel
	c.done = done
	c.healthDone = healthDone
	c.status = statusStarting
	c.startedAt = time.Now()
	c.lastErr = ""
	c.healthOK = false
	c.mu.Unlock()
	listener, err := net.Listen("tcp4", cfg.Listen)
	if err != nil {
		cancel()
		close(done)
		close(healthDone)
		err = listenError(err, cfg.Listen)
		c.fail(err)
		return err
	}
	c.mu.Lock()
	c.listener = listener
	c.mu.Unlock()
	c.notify()
	c.logf("INFO", fmt.Sprintf("memulai layanan pada %s", cfg.Listen))

	go func() {
		defer close(done)
		defer cancel()
		if err := server.App().Listener(listener); err != nil && ctx.Err() == nil {
			c.fail(listenError(err, cfg.Listen))
		}
	}()
	go func() {
		defer close(healthDone)
		c.watchHealth(ctx, done, cfg.Listen)
	}()
	return nil
}

func listenError(err error, listen string) error {
	// Windows mengembalikan WSAEADDRINUSE (10048), bukan syscall.EADDRINUSE.
	if errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.Errno(10048)) {
		return fmt.Errorf("alamat %s sedang dipakai proses lain. Tutup proses tersebut atau ubah port di Pengaturan > Alamat Dengarkan; sesuaikan juga URL pada klien: %w", listen, err)
	}
	return fmt.Errorf("layanan berhenti: %w", err)
}

// watchHealth memantau /health sampai layanan berhenti atau dimatikan.
func (c *ServiceController) watchHealth(ctx context.Context, done <-chan struct{}, listen string) {
	ticker := time.NewTicker(healthPollInterval)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
		}
		if probeHealth(client, listen) {
			misses = 0
			c.markHealthy()
			continue
		}
		misses++
		if misses >= 3 {
			c.markUnhealthy()
		}
	}
}

func probeHealth(client *http.Client, listen string) bool {
	response, err := client.Get("http://" + listen + "/health")
	if err != nil {
		return false
	}
	defer func() {
		_ = response.Body.Close()
	}()
	var health struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	return response.StatusCode == http.StatusOK &&
		json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health) == nil &&
		health.Status == "ok" && health.Service == "rsisa-print-server"
}

func (c *ServiceController) markHealthy() {
	c.mu.Lock()
	changed := !c.healthOK || c.status != statusRunning
	c.healthOK = true
	if c.status == statusStarting {
		c.status = statusRunning
	}
	state := c.stateLocked()
	c.mu.Unlock()
	if changed {
		if c.onState != nil {
			c.onState(state)
		}
	}
}

func (c *ServiceController) markUnhealthy() {
	c.mu.Lock()
	changed := c.healthOK
	c.healthOK = false
	state := c.stateLocked()
	c.mu.Unlock()
	if changed {
		if c.onState != nil {
			c.onState(state)
		}
	}
}

func (c *ServiceController) fail(err error) {
	c.mu.Lock()
	c.status = statusError
	c.healthOK = false
	c.lastErr = err.Error()
	state := c.stateLocked()
	c.mu.Unlock()
	c.logf("ERROR", err.Error())
	if c.onState != nil {
		c.onState(state)
	}
}

// Stop mematikan layanan secara graceful dan menghentikan seluruh goroutine
// pemantau.
func (c *ServiceController) Stop() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.stop()
}

func (c *ServiceController) stop() error {
	c.mu.Lock()
	if c.status == statusStopped {
		c.mu.Unlock()
		return nil
	}
	server, listener := c.server, c.listener
	cancel, done, healthDone := c.cancel, c.done, c.healthDone
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	ctx, timeout := context.WithTimeout(context.Background(), shutdownTimeout)
	defer timeout()
	var shutdownErr error
	if server != nil && listener != nil {
		shutdownErr = server.Shutdown(ctx)
		// Fiber mengembalikan error ini bila Stop menang lebih dulu daripada
		// goroutine yang baru memanggil App.Listener. Listener tetap harus
		// ditutup sendiri pada kasus tersebut.
		if shutdownErr != nil && strings.Contains(shutdownErr.Error(), "shutdown: server is not running") {
			shutdownErr = nil
			_ = listener.Close()
		}
		// Menutup listener yang sudah ditutup oleh Fiber adalah kondisi normal
		// saat shutdown bersamaan dengan Serve; jangan menjadikannya kegagalan
		// restart.
		if errors.Is(shutdownErr, net.ErrClosed) {
			shutdownErr = nil
		}
	}
	if shutdownErr != nil {
		c.fail(fmt.Errorf("shutdown layanan: %w", shutdownErr))
		return shutdownErr
	}
	if listener != nil {
		// Bila Fiber belum sempat mendaftarkan listener, Shutdown tidak dapat
		// menutupnya. Close idempoten secara logis untuk tujuan lifecycle ini;
		// error ErrClosed tidak relevan bagi caller.
		_ = listener.Close()
	}
	for _, stopped := range []chan struct{}{done, healthDone} {
		if stopped == nil {
			continue
		}
		select {
		case <-stopped:
		case <-ctx.Done():
			c.fail(fmt.Errorf("layanan belum selesai berhenti: %w", ctx.Err()))
			return ctx.Err()
		}
	}
	c.mu.Lock()
	c.server, c.listener = nil, nil
	c.cancel, c.done, c.healthDone = nil, nil, nil
	c.status, c.lastErr, c.healthOK = statusStopped, "", false
	state := c.stateLocked()
	c.mu.Unlock()
	c.logf("INFO", "layanan dihentikan")
	if c.onState != nil {
		c.onState(state)
	}
	return nil
}

// Restart menyelesaikan penghentian sebelum membuka listener berikutnya.
func (c *ServiceController) Restart() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err := c.stop(); err != nil {
		return err
	}
	return c.start()
}

// UpdateConfig menerapkan konfigurasi baru ke layanan yang sedang berjalan.
func (c *ServiceController) UpdateConfig(cfg printserver.Config) error {
	c.mu.Lock()
	server := c.server
	c.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.UpdateConfig(cfg)
}

// TestConnection memeriksa /health dan mengembalikan hasilnya untuk UI.
func TestConnection(listen string) HealthResult {
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	start := time.Now()
	ok := probeHealth(client, listen)
	latency := time.Since(start).Milliseconds()
	if ok {
		return HealthResult{OK: true, Message: "Print server merespons dengan sehat.", LatencyMS: latency}
	}
	return HealthResult{OK: false, Message: "Print server tidak merespons; coba mulai layanan.", LatencyMS: latency}
}

// ErrNotRunning dipakai bila operasi membutuhkan layanan yang sedang berjalan.
var ErrNotRunning = errors.New("layanan tidak sedang berjalan")
