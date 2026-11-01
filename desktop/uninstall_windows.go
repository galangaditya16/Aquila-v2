package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func prepareUninstall(executable string) (uninstallPlan, error) {
	uninstaller := filepath.Join(filepath.Dir(executable), "uninstall.exe")
	if info, err := os.Stat(uninstaller); err != nil || !info.Mode().IsRegular() {
		return uninstallPlan{}, fmt.Errorf("uninstaller tidak ditemukan. Untuk versi portable, keluar dari Aquila lalu hapus file %s", executable)
	}
	return uninstallPlan{
		description: "Buka uninstaller Windows untuk Aquila? Aplikasi ini akan ditutup. Lanjutkan penghapusan pada jendela uninstaller.",
		run: func() error {
			path, err := windows.UTF16PtrFromString(uninstaller)
			if err != nil {
				return err
			}
			return windows.ShellExecute(0, nil, path, nil, nil, windows.SW_SHOWNORMAL)
		},
	}, nil
}
