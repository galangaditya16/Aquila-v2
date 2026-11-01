package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/printserver"
)

const appVersion = "1.0.5"

// AppSettings adalah preferensi aplikasi desktop (terpisah dari
// print-server.json agar file konfigurasi print server tetap bersih).
type AppSettings struct {
	StartServiceOnLaunch bool `json:"start_service_on_launch"`
	OpenWindowOnLaunch   bool `json:"open_window_on_launch"`
	HideWindowOnClose    bool `json:"hide_window_on_close"`
	NotifyOnError        bool `json:"notify_on_error"`
	ConfirmQuit          bool `json:"confirm_quit"`
}

func DefaultSettings() AppSettings {
	return AppSettings{
		StartServiceOnLaunch: true,
		OpenWindowOnLaunch:   runtime.GOOS == "windows",
		HideWindowOnClose:    true,
		NotifyOnError:        true,
		ConfirmQuit:          true,
	}
}

// ConfigView adalah ringkasan konfigurasi untuk ditampilkan di UI.
type ConfigView struct {
	Path       string                   `json:"path"`
	Listen     string                   `json:"listen"`
	ListenHost string                   `json:"listen_host"`
	ListenPort int                      `json:"listen_port"`
	Origins    []printserver.OriginInfo `json:"origins"`
	JSON       string                   `json:"json"`
	Created    bool                     `json:"created"`
	Version    string                   `json:"version"`
}

// Core menyatukan konfigurasi, layanan, dan log aplikasi desktop.
type Core struct {
	paths   Paths
	logs    *LogBuffer
	service *ServiceController

	mu       sync.RWMutex
	cfg      printserver.Config
	settings AppSettings
	created  bool

	emit func(name string, data any)
}

func NewCore(paths Paths) (*Core, error) {
	created, err := printserver.EnsureConfigFile(paths.PrintServer)
	if err != nil {
		return nil, err
	}
	cfg, err := printserver.LoadConfig(paths.PrintServer)
	if err != nil {
		return nil, err
	}
	core := &Core{
		paths:   paths,
		logs:    NewLogBuffer(),
		cfg:     cfg,
		created: created,
	}
	core.settings = loadSettings(paths.Settings)
	core.service = NewServiceController(core.currentConfig, core.log, core.onServiceState)
	return core, nil
}

func loadSettings(path string) AppSettings {
	settings := DefaultSettings()
	contents, err := os.ReadFile(path)
	if err != nil {
		return settings
	}
	var loaded AppSettings
	if err := json.Unmarshal(contents, &loaded); err != nil {
		return settings
	}
	return loaded
}

func (c *Core) log(level, message string) {
	entry := c.logs.Add(level, message)
	if c.emit != nil {
		c.emit("aquila:log", entry)
	}
}

func (c *Core) onServiceState(state ServiceState) {
	if c.emit != nil {
		c.emit("aquila:state", state)
	}
}

func (c *Core) currentConfig() printserver.Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshotLocked()
}

// snapshotLocked menyalin konfigurasi; pemanggil memegang c.mu.
func (c *Core) snapshotLocked() printserver.Config {
	cfg := c.cfg
	cfg.AllowedOrigin = append([]string(nil), c.cfg.AllowedOrigin...)
	cfg.Printers = append([]printserver.PrinterConfig(nil), c.cfg.Printers...)
	cfg.Routes = append([]printserver.RouteConfig(nil), c.cfg.Routes...)
	return cfg
}

// commitLocked menyimpan konfigurasi ke file dan mengembalikan salinannya
// untuk diterapkan ke layanan setelah kunci dilepaskan. Pemanggil memegang
// c.mu (write lock) dan wajib menangani rollback bila terjadi error.
func (c *Core) commitLocked() (printserver.Config, error) {
	cfg := c.snapshotLocked()
	if err := printserver.SaveConfig(c.paths.PrintServer, cfg); err != nil {
		return printserver.Config{}, err
	}
	return cfg, nil
}

func (c *Core) applyToService(cfg printserver.Config) {
	if err := c.service.UpdateConfig(cfg); err != nil {
		c.log("ERROR", "menerapkan konfigurasi ke layanan: "+err.Error())
	}
}

// State mengembalikan keadaan layanan untuk UI.
func (c *Core) State() ServiceState { return c.service.State() }

func (c *Core) StartService() error { return c.service.Start() }

func (c *Core) StopService() error { return c.service.Stop() }

func (c *Core) RestartService() error { return c.service.Restart() }

func (c *Core) TestConnection() HealthResult {
	return TestConnection(c.currentConfig().Listen)
}

// Origins mengembalikan daftar origin ternormalisasi.
func (c *Core) Origins() []printserver.OriginInfo {
	cfg := c.currentConfig()
	infos := make([]printserver.OriginInfo, 0, len(cfg.AllowedOrigin))
	for _, raw := range cfg.AllowedOrigin {
		info, err := printserver.ParseOrigin(raw)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}
	return infos
}

func (c *Core) AddOrigin(scheme, host, port string) error {
	info, err := printserver.ComposeOrigin(scheme, host, port)
	if err != nil {
		return err
	}
	c.mu.Lock()
	for _, existing := range c.cfg.AllowedOrigin {
		if existing == info.Origin {
			c.mu.Unlock()
			return fmt.Errorf("alamat %s sudah terdaftar", info.Origin)
		}
	}
	c.cfg.AllowedOrigin = append(c.cfg.AllowedOrigin, info.Origin)
	cfg, err := c.commitLocked()
	if err != nil {
		last := len(c.cfg.AllowedOrigin) - 1
		c.cfg.AllowedOrigin = c.cfg.AllowedOrigin[:last]
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.applyToService(cfg)
	c.log("INFO", "alamat ditambahkan "+info.Origin)
	return nil
}

func (c *Core) UpdateOrigin(index int, scheme, host, port string) error {
	info, err := printserver.ComposeOrigin(scheme, host, port)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if index < 0 || index >= len(c.cfg.AllowedOrigin) {
		c.mu.Unlock()
		return fmt.Errorf("alamat tidak ditemukan")
	}
	for i, existing := range c.cfg.AllowedOrigin {
		if existing == info.Origin && i != index {
			c.mu.Unlock()
			return fmt.Errorf("alamat %s sudah terdaftar", info.Origin)
		}
	}
	previous := c.cfg.AllowedOrigin[index]
	c.cfg.AllowedOrigin[index] = info.Origin
	cfg, err := c.commitLocked()
	if err != nil {
		c.cfg.AllowedOrigin[index] = previous
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.applyToService(cfg)
	c.log("INFO", "alamat diubah "+previous+" -> "+info.Origin)
	return nil
}

func (c *Core) RemoveOrigin(index int) error {
	c.mu.Lock()
	if index < 0 || index >= len(c.cfg.AllowedOrigin) {
		c.mu.Unlock()
		return fmt.Errorf("alamat tidak ditemukan")
	}
	removed := c.cfg.AllowedOrigin[index]
	c.cfg.AllowedOrigin = append(c.cfg.AllowedOrigin[:index], c.cfg.AllowedOrigin[index+1:]...)
	cfg, err := c.commitLocked()
	if err != nil {
		restored := make([]string, 0, len(c.cfg.AllowedOrigin)+1)
		restored = append(restored, c.cfg.AllowedOrigin[:index]...)
		restored = append(restored, removed)
		restored = append(restored, c.cfg.AllowedOrigin[index:]...)
		c.cfg.AllowedOrigin = restored
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.applyToService(cfg)
	c.log("INFO", "alamat dihapus "+removed)
	return nil
}

// SetListen mengubah alamat dengarkan; bila restart true, layanan dimatikan
// lalu dijalankan ulang agar perubahan berlaku.
func (c *Core) SetListen(host string, port int, restart bool) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("host tidak boleh kosong")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port harus antara 1 dan 65535")
	}
	listen := host + ":" + strconv.Itoa(port)

	c.mu.Lock()
	previous := c.cfg.Listen
	c.cfg.Listen = listen
	if err := c.cfg.Validate(); err != nil {
		c.cfg.Listen = previous
		c.mu.Unlock()
		return err
	}
	cfg, err := c.commitLocked()
	if err != nil {
		c.cfg.Listen = previous
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.applyToService(cfg)
	c.log("INFO", "alamat dengarkan diubah menjadi "+listen)
	if restart && c.service.State().Status != statusStopped {
		return c.service.Restart()
	}
	return nil
}

// ConfigView membangun ringkasan konfigurasi untuk UI.
func (c *Core) ConfigView() ConfigView {
	cfg := c.currentConfig()
	host, portText, _ := strings.Cut(cfg.Listen, ":")
	port, _ := strconv.Atoi(portText)
	contents, err := os.ReadFile(c.paths.PrintServer)
	jsonText := ""
	if err == nil {
		jsonText = string(contents)
	}
	c.mu.RLock()
	created := c.created
	c.mu.RUnlock()
	return ConfigView{
		Path:       c.paths.PrintServer,
		Listen:     cfg.Listen,
		ListenHost: host,
		ListenPort: port,
		Origins:    c.Origins(),
		JSON:       jsonText,
		Created:    created,
		Version:    appVersion,
	}
}

// ResetConfig mengembalikan konfigurasi ke nilai bawaan.
func (c *Core) ResetConfig() error {
	c.mu.Lock()
	previous := c.snapshotLocked()
	c.cfg = printserver.DefaultConfig()
	cfg, err := c.commitLocked()
	if err != nil {
		c.cfg = previous
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.applyToService(cfg)
	c.log("INFO", "konfigurasi dikembalikan ke default")
	if c.service.State().Status != statusStopped {
		return c.service.Restart()
	}
	return nil
}

// ImportFrom membaca konfigurasi dari file luar (mis. print-server.json lama).
func (c *Core) ImportFrom(path string) error {
	cfg, err := printserver.LoadConfig(path)
	if err != nil {
		return err
	}
	c.mu.Lock()
	previous := c.snapshotLocked()
	c.cfg = cfg
	saved, err := c.commitLocked()
	if err != nil {
		c.cfg = previous
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	c.applyToService(saved)
	c.log("INFO", "konfigurasi diimpor dari "+path)
	if c.service.State().Status != statusStopped {
		return c.service.Restart()
	}
	return nil
}

// ExportTo menyimpan salinan konfigurasi ke tujuan pilihan pengguna.
func (c *Core) ExportTo(path string) error {
	if err := printserver.SaveConfig(path, c.currentConfig()); err != nil {
		return err
	}
	c.log("INFO", "konfigurasi diekspor ke "+path)
	return nil
}

// Reload memuat ulang konfigurasi dari file.
func (c *Core) Reload() error {
	cfg, err := printserver.LoadConfig(c.paths.PrintServer)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.cfg = cfg
	saved, err := c.commitLocked()
	c.mu.Unlock()
	if err != nil {
		return err
	}
	c.applyToService(saved)
	c.log("INFO", "konfigurasi dimuat ulang")
	return nil
}

// Settings mengembalikan preferensi aplikasi.
func (c *Core) Settings() AppSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.settings
}

// SaveSettings menyimpan preferensi aplikasi.
func (c *Core) SaveSettings(settings AppSettings) error {
	contents, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	if err := os.MkdirAll(c.paths.Dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(c.paths.Dir, ".app-settings-*.json")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(contents); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, c.paths.Settings); err != nil {
		return err
	}
	c.mu.Lock()
	c.settings = settings
	c.mu.Unlock()
	c.log("INFO", "pengaturan aplikasi disimpan")
	return nil
}

// Logs mengembalikan entri log terbaru.
func (c *Core) Logs() []LogEntry { return c.logs.Latest(maxLogRows) }

// ClearLogs mengosongkan log.
func (c *Core) ClearLogs() { c.logs.Clear() }

// PrinterCount menghitung antrean printer yang terdeteksi pada PC ini.
func (c *Core) PrinterCount() int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	printers, err := printserver.ListPrinters(ctx)
	if err != nil {
		return 0
	}
	return len(printers)
}

// Shutdown mematikan layanan sebelum aplikasi ditutup.
func (c *Core) Shutdown() {
	if err := c.service.Stop(); err != nil {
		c.log("ERROR", "gagal menghentikan layanan: "+err.Error())
	}
}

// ConfigPath mengembalikan lokasi print-server.json.
func (c *Core) ConfigPath() string { return c.paths.PrintServer }

// ConfigDir mengembalikan folder konfigurasi.
func (c *Core) ConfigDir() string { return c.paths.Dir }
