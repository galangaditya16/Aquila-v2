package main

import (
	"embed"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:assets
var trayAssets embed.FS

// trayUI mengelola ikon baki sistem, menu klik kanan, dan popup klik kiri
// (gaya Cloudflare WARP): klik kiri menampilkan popup ringkas, klik kanan
// menampilkan menu. Ikon dan tooltip berubah mengikuti keadaan layanan.
type trayUI struct {
	tray   *application.SystemTray
	status *application.MenuItem
	toggle *application.MenuItem

	iconIdle    []byte
	iconRunning []byte
	iconError   []byte

	ready     atomic.Bool
	destroyed atomic.Bool
}

// markReady dipanggil saat aplikasi sudah berjalan; sebelum itu dispatch ke
// utas utama belum tersedia sehingga update harus ditahan.
func (t *trayUI) markReady() { t.ready.Store(true) }

func newTrayUI(app *application.App, api *AquilaService, popup *application.WebviewWindow) *trayUI {
	t := &trayUI{
		tray:        app.SystemTray.New(),
		iconIdle:    readTrayIcon("assets/tray-idle.png"),
		iconRunning: readTrayIcon("assets/tray-running.png"),
		iconError:   readTrayIcon("assets/tray-error.png"),
	}

	menu := app.NewMenu()
	t.status = menu.Add("Layanan berhenti")
	t.status.SetEnabled(false)
	menu.AddSeparator()
	openItem := menu.Add("Buka Panel")
	openItem.OnClick(func(*application.Context) {
		go api.ShowPanel()
	})
	t.toggle = menu.Add("Mulai Layanan")
	t.toggle.OnClick(func(*application.Context) {
		go func() {
			if err := api.ToggleService(); err != nil {
				api.core.log("ERROR", err.Error())
			}
		}()
	})
	menu.AddSeparator()
	quitItem := menu.Add("Keluar")
	quitItem.OnClick(func(*application.Context) {
		go api.QuitApp()
	})

	t.tray.SetIcon(t.iconIdle)
	t.tray.SetTooltip("Aquila")
	t.tray.SetMenu(menu)
	t.tray.AttachWindow(popup)
	t.tray.WindowOffset(8)
	return t
}

// update menyegarkan ikon, tooltip, dan label menu sesuai keadaan layanan.
// Aman dipanggil dari goroutine mana pun selain utas utama.
func (t *trayUI) update(state ServiceState) {
	if t.destroyed.Load() || !t.ready.Load() {
		return
	}
	icon := t.iconIdle
	statusText := "Layanan berhenti"
	toggleText := "Mulai Layanan"
	switch state.Status {
	case statusStarting:
		statusText = "Layanan memulai…"
		toggleText = "Hentikan Layanan"
	case statusRunning:
		toggleText = "Hentikan Layanan"
		icon = t.iconRunning
		statusText = "Layanan berjalan di " + state.Listen
		if !state.HealthOK {
			icon = t.iconError
			statusText = "Layanan tidak merespons di " + state.Listen
		}
	case statusError:
		icon = t.iconError
		statusText = "Layanan bermasalah"
		if state.LastError != "" {
			statusText = "Layanan bermasalah: " + state.LastError
		}
		toggleText = "Coba Lagi"
	}
	if icon != nil {
		t.tray.SetIcon(icon)
	}
	t.tray.SetTooltip("Aquila — " + statusText)
	application.InvokeAsync(func() {
		t.status.SetLabel(statusText)
		t.toggle.SetLabel(toggleText)
	})
}

// stop menandai tray sudah tidak boleh diperbarui lagi (saat shutdown).
func (t *trayUI) stop() { t.destroyed.Store(true) }

// Destroy melepaskan sumber daya tray dari sistem.
func (t *trayUI) Destroy() {
	t.destroyed.Store(true)
	t.tray.Destroy()
}

func readTrayIcon(name string) []byte {
	data, err := trayAssets.ReadFile(name)
	if err != nil {
		return nil
	}
	return data
}
