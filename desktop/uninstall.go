package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type uninstallPlan struct {
	description      string
	disableAutostart bool
	run              func() error
}

// UninstallApp meminta konfirmasi sebelum menjalankan penghapusan aplikasi.
// Konfigurasi pengguna tidak dihapus.
func (s *AquilaService) uninstallApp() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	plan, err := prepareUninstall(executable)
	if err != nil {
		return err
	}
	app := s.app()
	dialog := app.Dialog.Question()
	dialog.SetTitle("Uninstall Aquila")
	dialog.SetMessage(plan.description + "\n\nLayanan akan dihentikan dan startup otomatis dinonaktifkan. Konfigurasi tetap disimpan di:\n" + s.core.ConfigDir())
	confirm := dialog.AddButton("Uninstall")
	confirm.OnClick(func() {
		go func() {
			if err := s.runUninstall(plan); err != nil {
				s.core.log("ERROR", "uninstall gagal: "+err.Error())
				app.Dialog.Error().SetTitle("Aquila belum dihapus").SetMessage(err.Error()).Show()
				return
			}
			quitting.Store(true)
			app.Quit()
		}()
	})
	cancel := dialog.AddButton("Batal")
	dialog.SetDefaultButton(cancel)
	dialog.SetCancelButton(cancel)
	dialog.Show()
	return nil
}

func (s *AquilaService) runUninstall(plan uninstallPlan) (err error) {
	app := s.app()
	enabled := false
	if plan.disableAutostart {
		var err error
		enabled, err = app.Autostart.IsEnabled()
		if err != nil {
			return fmt.Errorf("periksa startup otomatis: %w", err)
		}
		if err := app.Autostart.Disable(); err != nil {
			return fmt.Errorf("nonaktifkan startup otomatis: %w", err)
		}
	}
	status := s.core.State().Status
	wasRunning := status == statusRunning || status == statusStarting
	// Batalkan perubahan layanan/startup bila penghapusan gagal atau dibatalkan.
	defer func() {
		if err == nil {
			return
		}
		if plan.disableAutostart && enabled {
			err = errors.Join(err, app.Autostart.Enable())
		}
		if wasRunning {
			err = errors.Join(err, s.core.StartService())
		}
	}()
	if err := s.core.StopService(); err != nil {
		return err
	}
	return plan.run()
}
