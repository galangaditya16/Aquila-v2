package printserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/utils"
)

func TestParseWindowsPrintersIncludesQueuePortAndIP(t *testing.T) {
	got, err := parseWindowsPrinters([]byte(`[{"name":"Epson L5190","state":"ready","is_default":true,"driver":"Epson Driver","port":"IP_192.168.1.20","ip":"192.168.1.20"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Epson L5190" || got[0].Port != "IP_192.168.1.20" || got[0].IP != "192.168.1.20" || got[0].URI != utils.WindowsQueueURI("Epson L5190") {
		t.Fatalf("parsed printer = %+v", got)
	}
	encoded, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"name":"Epson L5190"`, `"port":"IP_192.168.1.20"`, `"ip":"192.168.1.20"`, `"uri":"` + utils.WindowsQueueURI("Epson L5190") + `"`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("response omitted %s: %s", field, encoded)
		}
	}
}

func TestParseWindowsPrintersAllowsQueuesWithoutNetworkIP(t *testing.T) {
	got, err := parseWindowsPrinters([]byte(`[{"name":"USB Printer","state":"ready","port":"USB001","ip":null}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "USB Printer" || got[0].Port != "USB001" || got[0].IP != "" {
		t.Fatalf("parsed printer = %+v", got)
	}
}

func TestParseWindowsPrintersSingleObject(t *testing.T) {
	got, err := parseWindowsPrinters([]byte(`{"name":"L3150 Series(Network)","state":"ready","is_default":true,"driver":"EPSON L3150 Series","port":"IP_192.168.1.50","ip":"192.168.1.50"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "L3150 Series(Network)" || got[0].Port != "IP_192.168.1.50" || got[0].URI != utils.WindowsQueueURI("L3150 Series(Network)") {
		t.Fatalf("parsed single printer = %+v", got)
	}
}

func TestParseWindowsPrintersHandlesSingleObject(t *testing.T) {
	singleJSON := `{"name":"Epson L5190","state":"ready","is_default":true,"driver":"Epson Driver","port":"USB001","ip":null}`
	got, err := parseWindowsPrinters([]byte(singleJSON))
	if err != nil {
		t.Fatalf("gagal membaca printer tunggal: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Epson L5190" {
		t.Fatalf("hasil parse tidak valid: %+v", got)
	}
}

func TestLocalPrinterOmitsWindowsMetadataWhenUnavailable(t *testing.T) {
	encoded, err := json.Marshal(LocalPrinter{Name: "USB Printer", State: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"port"`, `"ip"`, `"uri"`} {
		if strings.Contains(string(encoded), key) {
			t.Fatalf("optional metadata %s unexpectedly present: %s", key, encoded)
		}
	}
}
