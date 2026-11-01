package main

import (
	"os"
	"path/filepath"
)

// Paths menentukan lokasi file konfigurasi aplikasi desktop. File
// print-server.json selalu diberi nama tersebut agar tetap kompatibel dengan
// aplikasi farmasi dan CLI print server.
type Paths struct {
	Dir         string // folder konfigurasi milik pengguna
	PrintServer string // print-server.json
	Settings    string // app-settings.json
}

// ResolvePaths menentukan lokasi konfigurasi. Variabel lingkungan
// RSISA_PRINT_SERVER_CONFIG tetap dihormati bila disetel.
func ResolvePaths() (Paths, error) {
	if override := os.Getenv("RSISA_PRINT_SERVER_CONFIG"); override != "" {
		dir := filepath.Dir(override)
		return Paths{Dir: dir, PrintServer: override, Settings: filepath.Join(dir, "app-settings.json")}, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	dir := filepath.Join(base, "Aquila")
	return Paths{
		Dir:         dir,
		PrintServer: filepath.Join(dir, "print-server.json"),
		Settings:    filepath.Join(dir, "app-settings.json"),
	}, nil
}
