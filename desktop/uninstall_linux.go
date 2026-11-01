package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func prepareUninstall(executable string) (uninstallPlan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	manager, argument := "", ""
	if output, err := exec.CommandContext(ctx, "dpkg-query", "-S", executable).Output(); err == nil && strings.TrimSpace(string(output)) == "aquila: "+executable {
		manager, argument = "dpkg", "--remove"
	} else if output, err := exec.CommandContext(ctx, "rpm", "-qf", "--qf", "%{NAME}", executable).Output(); err == nil && string(output) == "aquila" {
		manager, argument = "rpm", "-e"
	}
	if manager == "" {
		return uninstallPlan{}, fmt.Errorf("Aquila ini tidak terdaftar sebagai paket deb/rpm. Untuk versi portable, keluar dari Aquila lalu hapus file %s", executable)
	}
	managerPath, err := exec.LookPath(manager)
	if err != nil {
		return uninstallPlan{}, err
	}
	pkexec, err := exec.LookPath("pkexec")
	if err != nil {
		return uninstallPlan{}, fmt.Errorf("dialog izin administrator tidak tersedia. Keluar dari Aquila lalu jalankan di terminal: sudo %s %s aquila", manager, argument)
	}
	return uninstallPlan{
		description:      "Hapus paket Aquila melalui pengelola paket Linux? Sistem akan meminta izin administrator.",
		disableAutostart: true,
		run: func() error {
			output, err := exec.Command(pkexec, managerPath, argument, "aquila").CombinedOutput()
			if err != nil {
				return fmt.Errorf("penghapusan gagal atau dibatalkan: %s (%w)", strings.TrimSpace(string(output)), err)
			}
			return nil
		},
	}, nil
}
