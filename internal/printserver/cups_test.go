package printserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalCUPSQueue(t *testing.T) {
	for uri, want := range map[string]string{
		"ipp://127.0.0.1:631/printers/EPSON_L5190_Series": "EPSON_L5190_Series",
		"ipp://[::1]:631/printers/EPSON":                  "EPSON",
		"ipp://192.168.1.12:631/ipp/print":                "",
		"ipps://127.0.0.1:631/printers/EPSON":             "",
		"ipp://127.0.0.1:632/printers/EPSON":              "",
		"ipp://127.0.0.1:631/printers/EPSON/other":        "",
		"ipp://127.0.0.1:631/printers/":                   "",
		"ipp://127.0.0.1:631/printers/EPSON?other=true":   "",
	} {
		if got := localCUPSQueue(uri); got != want {
			t.Errorf("localCUPSQueue(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestPrintPDFToCUPSUsesLocalQueue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("lp is available on macOS and Linux")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	bodyFile := filepath.Join(dir, "body")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$AQUILA_LP_ARGS\"\nfor arg in \"$@\"; do file=\"$arg\"; done\ncat \"$file\" > \"$AQUILA_LP_BODY\"\n"
	if err := os.WriteFile(filepath.Join(dir, "lp"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AQUILA_LP_ARGS", argsFile)
	t.Setenv("AQUILA_LP_BODY", bodyFile)
	pdf := []byte("%PDF-1.4\n%%EOF")
	request := httptest.NewRequest(http.MethodPost, "/print/Billing?module_code=resep_rajal", strings.NewReader(string(pdf)))
	request.Header.Set("X-RSISA-Printer-URI", "ipp://127.0.0.1:631/printers/EPSON_L5190_Series")
	request.Header.Set("X-RSISA-Copies", "2")
	request.Header.Set("X-RSISA-Media", "A4")
	response, err := New(Config{}, func(string, string) {}).App().Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("print status = %d, want 202", response.StatusCode)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(args), "-d\nEPSON_L5190_Series\n-t\nBilling\n-n\n2\n-o\njob-sheets=none\n-o\nmedia=A4\n") {
		t.Fatalf("unexpected lp arguments: %q", args)
	}
	body, err := os.ReadFile(bodyFile)
	if err != nil || string(body) != string(pdf) {
		t.Fatalf("printed PDF = %q, err = %v", body, err)
	}
	if err := printPDFToCUPS(context.Background(), "EPSON_L5190_Series", pdf, "Billing", 1, "A4 -o raw"); err == nil {
		t.Fatal("media containing extra options was accepted")
	}
}
