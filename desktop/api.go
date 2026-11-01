package main

import (
	"fmt"
	"sync/atomic"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/printserver"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// quitting menandai aplikasi sedang ditutup sungguhan, sehingga hook
// WindowClosing tidak lagi menahan jendela untuk disembunyikan ke tray.
var quitting atomic.Bool

// AquilaService mengekspos API aplikasi desktop ke frontend. Seluruh method
// yang diekspor dijadikan binding JavaScript oleh wails3 generate bindings.
type AquilaService struct {
	core *Core
}

func NewAquilaService(core *Core) *AquilaService {
	return &AquilaService{core: core}
}

func (s *AquilaService) app() *application.App { return application.Get() }

// ---------------------------------------------------------------- layanan --

// GetState mengembalikan keadaan layanan print server saat ini.
func (s *AquilaService) GetState() ServiceState { return s.core.State() }

// StartService menjalankan print server.
func (s *AquilaService) StartService() error { return s.core.StartService() }

// StopService menghentikan print server.
func (s *AquilaService) StopService() error { return s.core.StopService() }

// RestartService menjalankan ulang print server dengan konfigurasi terbaru.
func (s *AquilaService) RestartService() error { return s.core.RestartService() }

// TestConnection memeriksa kesehatan print server lokal.
func (s *AquilaService) TestConnection() HealthResult { return s.core.TestConnection() }

// DiagnosePort mengembalikan nama proses yang memakai port layanan saat ini.
// Hasil ini dipakai saat layanan gagal start karena port sedang digunakan.
func (s *AquilaService) DiagnosePort() (PortOwnerResult, error) {
	return DiagnosePort(s.core.currentConfig().Listen)
}

// ----------------------------------------------------------------- origin --

// GetOrigins mengembalikan daftar alamat asal yang diizinkan.
func (s *AquilaService) GetOrigins() []printserver.OriginInfo { return s.core.Origins() }

// AddOrigin menambahkan alamat asal baru (domain, subdomain, atau host:port).
func (s *AquilaService) AddOrigin(scheme, host, port string) error {
	return s.core.AddOrigin(scheme, host, port)
}

// UpdateOrigin mengubah alamat asal pada posisi index.
func (s *AquilaService) UpdateOrigin(index int, scheme, host, port string) error {
	return s.core.UpdateOrigin(index, scheme, host, port)
}

// RemoveOrigin menghapus alamat asal pada posisi index.
func (s *AquilaService) RemoveOrigin(index int) error { return s.core.RemoveOrigin(index) }

// ---------------------------------------------------------------- konfig ---

// GetConfig mengembalikan ringkasan konfigurasi print-server.json.
func (s *AquilaService) GetConfig() ConfigView { return s.core.ConfigView() }

// SetListen mengubah alamat dengarkan; bila restart true, layanan dimatikan
// lalu dijalankan ulang agar perubahan berlaku.
func (s *AquilaService) SetListen(host string, port int, restart bool) error {
	return s.core.SetListen(host, port, restart)
}

// ResetConfig mengembalikan konfigurasi ke nilai bawaan.
func (s *AquilaService) ResetConfig() error { return s.core.ResetConfig() }

// ReloadConfig memuat ulang konfigurasi dari file.
func (s *AquilaService) ReloadConfig() error { return s.core.Reload() }

// ImportConfig membuka dialog pilih file lalu mengimpor konfigurasi darinya.
func (s *AquilaService) ImportConfig() error {
	path, err := s.app().Dialog.OpenFile().
		SetTitle("Impor Konfigurasi").
		SetMessage("Pilih file print-server.json yang akan diimpor").
		SetDirectory(s.core.ConfigDir()).
		AddFilter("Konfigurasi JSON", "*.json").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		PromptForSingleSelection()
	if err != nil {
		return err
	}
	if path == "" {
		return nil // dibatalkan pengguna
	}
	return s.core.ImportFrom(path)
}

// ExportConfig membuka dialog simpan lalu menyalin konfigurasi ke sana.
func (s *AquilaService) ExportConfig() error {
	path, err := s.app().Dialog.SaveFile().
		SetMessage("Simpan salinan konfigurasi").
		SetDirectory(s.core.ConfigDir()).
		SetFilename("print-server.json").
		AddFilter("Konfigurasi JSON", "*.json").
		PromptForSingleSelection()
	if err != nil {
		return err
	}
	if path == "" {
		return nil // dibatalkan pengguna
	}
	return s.core.ExportTo(path)
}

// -------------------------------------------------------------- pengaturan --

// GetSettings mengembalikan preferensi aplikasi desktop.
func (s *AquilaService) GetSettings() AppSettings { return s.core.Settings() }

// SaveSettings menyimpan preferensi aplikasi desktop.
func (s *AquilaService) SaveSettings(settings AppSettings) error {
	return s.core.SaveSettings(settings)
}

// UninstallApp meminta konfirmasi sebelum menghapus aplikasi.
func (s *AquilaService) UninstallApp() error { return s.uninstallApp() }

// GetAutostart melaporkan apakah aplikasi otomatis jalan saat login.
func (s *AquilaService) GetAutostart() (bool, error) { return s.app().Autostart.IsEnabled() }

// SetAutostart mengaktifkan atau menonaktifkan autostart saat login.
func (s *AquilaService) SetAutostart(enabled bool) error {
	if enabled {
		return s.app().Autostart.Enable()
	}
	return s.app().Autostart.Disable()
}

// -------------------------------------------------------------------- log --

// GetLogs mengembalikan entri log terbaru (lama ke baru).
func (s *AquilaService) GetLogs() []LogEntry { return s.core.Logs() }

// ClearLogs mengosongkan log.
func (s *AquilaService) ClearLogs() { s.core.ClearLogs() }

// ----------------------------------------------------------------- umum ----

// OpenConfigFolder membuka folder konfigurasi di file manager.
func (s *AquilaService) OpenConfigFolder() error {
	return s.app().Env.OpenFileManager(s.core.ConfigDir(), false)
}

// CopyServerAddress menyalin alamat print server ke papan klip.
func (s *AquilaService) CopyServerAddress() error {
	address := "http://" + s.core.currentConfig().Listen
	if !s.app().Clipboard.SetText(address) {
		return fmt.Errorf("tidak dapat menyalin %s ke papan klip", address)
	}
	s.core.log("INFO", "alamat disalin ke papan klip: "+address)
	return nil
}

// OpenServerURL membuka pemeriksaan kesehatan print server di peramban.
func (s *AquilaService) OpenServerURL() error {
	if s.core.State().Status != statusRunning {
		return fmt.Errorf("layanan belum berjalan; mulai layanan terlebih dahulu")
	}
	return s.app().Browser.OpenURL("http://" + s.core.currentConfig().Listen + "/health")
}

// GetPrinterCount menghitung antrean printer yang terdeteksi pada PC ini.
func (s *AquilaService) GetPrinterCount() int { return s.core.PrinterCount() }

// ToggleService menghidupkan layanan bila berhenti, atau mematikannya bila
// sedang berjalan. Dipakai tombol popup dan menu tray.
func (s *AquilaService) ToggleService() error {
	state := s.core.State()
	if state.Status == statusRunning || state.Status == statusStarting {
		return s.core.StopService()
	}
	return s.core.StartService()
}

// ShowPanel menampilkan jendela utama (panel) di atas aplikasi lain.
func (s *AquilaService) ShowPanel() {
	app := s.app()
	window, ok := app.Window.GetByName("main")
	if !ok {
		return
	}
	application.InvokeAsync(func() {
		window.Show()
		window.Focus()
	})
	s.HidePopup()
}

// HidePopup menyembunyikan popup ringkas di baki sistem.
func (s *AquilaService) HidePopup() {
	window, ok := s.app().Window.GetByName("popup")
	if !ok {
		return
	}
	application.InvokeAsync(func() {
		window.Hide()
	})
}

// QuitApp menutup aplikasi (dengan konfirmasi bila diatur) dan menghentikan
// layanan print server.
func (s *AquilaService) QuitApp() {
	app := s.app()
	if s.core.Settings().ConfirmQuit {
		dialog := app.Dialog.Question()
		dialog.SetTitle("Keluar dari Aquila")
		dialog.SetMessage("Layanan print server akan dihentikan. Keluar sekarang?")
		confirm := dialog.AddButton("Keluar")
		confirm.OnClick(func() {
			quitting.Store(true)
			app.Quit()
		})
		cancel := dialog.AddButton("Batal")
		cancel.SetAsCancel()
		dialog.SetDefaultButton(cancel)
		dialog.SetCancelButton(cancel)
		dialog.Show()
		return
	}
	quitting.Store(true)
	app.Quit()
}
