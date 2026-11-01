package printserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseOriginNormalizes(t *testing.T) {
	cases := []struct {
		raw     string
		origin  string
		typ     string
		wantErr bool
	}{
		{"https://rsisa.id", "https://rsisa.id", "domain", false},
		{"https://serah-studio.my.id", "https://serah-studio.my.id", "domain", false},
		{"https://farmasi.example.test", "https://farmasi.example.test", "subdomain", false},
		{"https://fifo-rsisa.serah-studio.my.id/", "https://fifo-rsisa.serah-studio.my.id", "subdomain", false},
		{"http://localhost:4200", "http://localhost:4200", "local", false},
		{"http://192.168.10.25:8080", "http://192.168.10.25:8080", "ip", false},
		{"HTTPS://FARMASI.RSISA.ID:443", "https://farmasi.rsisa.id", "subdomain", false},
		{"http://localhost:80", "http://localhost", "local", false},
		{"http://[::1]:5173", "http://[::1]:5173", "local", false},
		{"https://apotek.farmasi.rsisa.id", "https://apotek.farmasi.rsisa.id", "subdomain", false},
		{"http://0.0.0.0:8080", "http://0.0.0.0:8080", "ip", false},
		{"https://farmasi.rsisa.id/login", "", "", true},
		{"ftp://192.168.10.5", "", "", true},
		{"farmasi.rsisa.id", "", "", true},
		{"http://host with space", "", "", true},
		{"http://", "", "", true},
		{"http://localhost:99999", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range cases {
		info, err := ParseOrigin(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseOrigin(%q) seharusnya gagal, dapat %q", tc.raw, info.Origin)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseOrigin(%q) gagal: %v", tc.raw, err)
			continue
		}
		if info.Origin != tc.origin {
			t.Errorf("ParseOrigin(%q) = %q, ingin %q", tc.raw, info.Origin, tc.origin)
		}
		if info.Type != tc.typ {
			t.Errorf("ParseOrigin(%q) type = %q, ingin %q", tc.raw, info.Type, tc.typ)
		}
	}
}

func TestComposeOrigin(t *testing.T) {
	info, err := ComposeOrigin("https", "Farmasi.RSISA.id", "8080")
	if err != nil {
		t.Fatal(err)
	}
	if info.Origin != "https://farmasi.rsisa.id:8080" {
		t.Fatalf("origin = %q", info.Origin)
	}
	info, err = ComposeOrigin("https", "farmasi.rsisa.id", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Origin != "https://farmasi.rsisa.id" {
		t.Fatalf("origin = %q", info.Origin)
	}
	if _, err := ComposeOrigin("ftp", "farmasi.rsisa.id", ""); err == nil {
		t.Fatal("skema ftp seharusnya ditolak")
	}
	if _, err := ComposeOrigin("https", "farmasi..rsisa", ""); err == nil {
		t.Fatal("host tidak valid seharusnya ditolak")
	}
}

func TestNormalizeOriginsDropsDuplicates(t *testing.T) {
	got, err := NormalizeOrigins([]string{
		"https://farmasi.example.test",
		"https://farmasi.example.test/",
		"http://localhost:4200",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "https://farmasi.example.test" || got[1] != "http://localhost:4200" {
		t.Fatalf("hasil = %#v", got)
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Listen != DefaultListen {
		t.Fatalf("listen = %q", cfg.Listen)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedOrigin) != 2 {
		t.Fatalf("allowed_origins = %#v", cfg.AllowedOrigin)
	}
}

func TestEnsureAndSaveConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-server.json")
	created, err := EnsureConfigFile(path)
	if err != nil || !created {
		t.Fatalf("EnsureConfigFile: created=%v err=%v", created, err)
	}
	created, err = EnsureConfigFile(path)
	if err != nil || created {
		t.Fatalf("EnsureConfigFile kedua: created=%v err=%v", created, err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Listen != DefaultListen || len(loaded.AllowedOrigin) != 2 {
		t.Fatalf("konfigurasi termuat = %#v", loaded)
	}

	loaded.AllowedOrigin = append(loaded.AllowedOrigin, "http://192.168.10.25:8080")
	if err := SaveConfig(path, loaded); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.AllowedOrigin) != 3 {
		t.Fatalf("allowed_origins = %#v", reloaded.AllowedOrigin)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".print-server-*.json")); len(matches) != 0 {
		t.Fatalf("file sementara tertinggal: %v", matches)
	}
}

func TestSaveConfigRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "print-server.json")
	cfg := DefaultConfig()
	cfg.Listen = "0.0.0.0:17891"
	if err := SaveConfig(path, cfg); err == nil {
		t.Fatal("listen non-localhost seharusnya ditolak")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("file tidak boleh tertulis bila konfigurasi tidak valid")
	}
}
