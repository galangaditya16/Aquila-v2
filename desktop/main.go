package main

import (
	"embed"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Wails menyematkan seluruh isi frontend/dist ke dalam binary.
//
//go:embed all:frontend/dist
var frontendAssets embed.FS

func init() {
	// Mendaftarkan event ber-tipe agar binding generator membuatkan API
	// JavaScript yang kuat untuk keduanya.
	application.RegisterEvent[ServiceState]("aquila:state")
	application.RegisterEvent[LogEntry]("aquila:log")
}

func main() {
	defer startupRecover()
	paths, err := ResolvePaths()
	if err != nil {
		startupFatal(err)
	}
	core, err := NewCore(paths)
	if err != nil {
		startupFatal(err)
	}
	api := NewAquilaService(core)
	settings := core.Settings()
	var ready atomic.Bool

	app := application.New(application.Options{
		Name:         "Aquila",
		Description:  "Panel kendali print server lokal Aquila",
		ErrorHandler: startupErrorHandler(),
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.aquila.printserver",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if ready.Load() {
					api.ShowPanel()
				}
			},
		},
		Services: []application.Service{
			application.NewService(api),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontendAssets),
		},
		Mac: application.MacOptions{
			// Aplikasi tetap hidup di baki sistem saat jendela ditutup.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})

	// Jendela utama (panel penuh). Pada instalasi pertama selalu dibuka agar
	// pengguna langsung melihat konfigurasi yang dibuatkan.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Aquila — Print Server",
		Width:            960,
		Height:           660,
		MinWidth:         720,
		MinHeight:        520,
		Hidden:           !(settings.OpenWindowOnLaunch || core.ConfigView().Created),
		BackgroundColour: application.NewRGB(248, 250, 252),
		URL:              "/",
	})

	// Popup ringkas untuk klik kiri pada tray.
	popup := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "popup",
		Title:            "Aquila",
		Width:            320,
		Height:           380,
		Frameless:        true,
		DisableResize:    true,
		AlwaysOnTop:      true,
		Hidden:           true,
		BackgroundColour: application.NewRGB(248, 250, 252),
		URL:              "/?popup=1",
	})

	tray := newTrayUI(app, api, popup)

	// Satu jalur emisi: event ke frontend + penyegaran tray.
	core.emit = func(name string, data any) {
		app.Event.Emit(name, data)
		if state, ok := data.(ServiceState); ok {
			tray.update(state)
		}
	}

	// Tutup jendela utama = sembunyikan ke tray (kecuali saat benar-benar keluar).
	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if quitting.Load() {
			return
		}
		e.Cancel()
		if core.Settings().HideWindowOnClose {
			mainWindow.Hide()
			return
		}
		go api.QuitApp()
	})

	popup.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if quitting.Load() {
			return
		}
		e.Cancel()
		popup.Hide()
	})

	app.OnShutdown(func() {
		ready.Store(false)
		tray.stop()
		core.Shutdown()
		tray.Destroy()
	})

	// Layanan baru boleh dimulai setelah aplikasi benar-benar berjalan:
	// event/tray dispatch belum tersedia sebelum app.Run().
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		tray.markReady()
		ready.Store(true)
		if settings.StartServiceOnLaunch {
			if err := core.StartService(); err != nil {
				core.log("ERROR", "gagal memulai layanan: "+err.Error())
			}
		}
	})

	if err := app.Run(); err != nil {
		startupFatal(err)
	}
	if !quitting.Load() {
		reportUnexpectedExit()
	}
}
