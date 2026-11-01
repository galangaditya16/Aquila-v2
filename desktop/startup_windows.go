//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func startupErrorHandler() func(error) {
	return func(err error) {
		showStartupError("Aquila gagal dijalankan:\n\n" + err.Error())
	}
}

func startupFatal(err error) {
	showStartupError("Aquila gagal dijalankan:\n\n" + err.Error())
	os.Exit(1)
}

func startupRecover() {
	if value := recover(); value != nil {
		startupFatal(fmt.Errorf("kesalahan saat membuka aplikasi: %v", value))
	}
}

func reportUnexpectedExit() {
	showStartupError("Aquila berhenti sebelum jendela dapat ditampilkan. Mohon kirim foto pesan ini kepada pengelola Aquila.")
}

func showStartupError(message string) {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	title, _ := windows.UTF16PtrFromString("Aquila")
	_, _ = windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR|windows.MB_SYSTEMMODAL)
}
