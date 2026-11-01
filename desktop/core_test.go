package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/printserver"
)

func TestExistingConfigOriginsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{Dir: dir, PrintServer: filepath.Join(dir, "print-server.json"), Settings: filepath.Join(dir, "app-settings.json")}
	cfg := printserver.DefaultConfig()
	cfg.AllowedOrigin = []string{"https://farmasi.example.test", "http://localhost:4200"}
	if err := printserver.SaveConfig(paths.PrintServer, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.PrintServer)
	if err != nil {
		t.Fatal(err)
	}
	core, err := NewCore(paths)
	if err != nil {
		t.Fatal(err)
	}
	view := core.ConfigView()
	if view.Created || len(view.Origins) != 2 || view.Origins[1].Origin != "http://localhost:4200" {
		t.Fatalf("existing configuration was not restored: %+v", view)
	}
	if err := core.AddOrigin("https", "farmasi.example.test", "443"); err == nil {
		t.Fatal("duplicate origin accepted")
	}
	after, err := os.ReadFile(paths.PrintServer)
	if err != nil || string(before) != string(after) {
		t.Fatalf("duplicate changed persisted config: %v", err)
	}
}
