package utils

import (
	"context"
	"encoding/binary"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIPPSExceptionRequiresExactLocalEndpoint(t *testing.T) {
	t.Setenv("RSISA_INSECURE_IPPS_HOST", "192.168.8.108:631")
	for _, tc := range []struct {
		uri  string
		want bool
	}{
		{"ipps://192.168.8.108:631/ipp/print", true},
		{"ipps://192.168.8.108/ipp/print", true},
		{"ipps://192.168.8.109:631/ipp/print", false},
		{"ipps://192.168.8.108:443/ipp/print", false},
		{"ipp://192.168.8.108:631/ipp/print", false},
	} {
		u, _ := url.Parse(tc.uri)
		if got := allowUnverifiedIPPS(u); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.uri, got, tc.want)
		}
	}
	for _, host := range []string{"8.8.8.8:631", "printer.example:631", "*:631"} {
		t.Setenv("RSISA_INSECURE_IPPS_HOST", host)
		u, _ := url.Parse("ipps://" + host + "/ipp/print")
		if allowUnverifiedIPPS(u) {
			t.Fatalf("non-local or wildcard exception accepted: %s", host)
		}
	}
}

func TestIPPSExceptionDoesNotBypassPDFCapabilityCheck(t *testing.T) {
	var printJobs atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) < 8 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if binary.BigEndian.Uint16(body[2:4]) == ippOperationPrintJob {
			printJobs.Add(1)
		}
		response := ippRequestHeaderVersion(ippStatusOK, binary.BigEndian.Uint32(body[4:8]), 2, 0)
		response = append(response, ippTagOperationAttributes)
		response = appendIPPAttribute(response, ippTagMimeMediaType, "document-format-supported", "image/pwg-raster")
		_, _ = w.Write(append(response, ippTagEnd))
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	u, _ := url.Parse(server.URL)
	printerURI := "ipps://" + u.Host + "/ipp/print"

	t.Setenv("RSISA_INSECURE_IPPS_HOST", "")
	if _, err := InspectIPPPrinter(context.Background(), printerURI); err == nil {
		t.Fatal("untrusted certificate accepted without an explicit exception")
	}
	t.Setenv("RSISA_INSECURE_IPPS_HOST", u.Host)
	caps, err := InspectIPPPrinter(context.Background(), printerURI)
	if err != nil || caps.PDFSupported {
		t.Fatalf("exception failed or invented PDF support: %+v, %v", caps, err)
	}
	_, err = PrintIPPDocument(context.Background(), printerURI, []byte("%PDF-1.4"), "Test", "rsisa", 1, "")
	if err == nil || !strings.Contains(err.Error(), "application/pdf") || printJobs.Load() != 0 {
		t.Fatalf("unsupported PDF was not rejected before sending: jobs=%d, err=%v", printJobs.Load(), err)
	}
}

func TestIPPSExceptionDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	t.Setenv("RSISA_INSECURE_IPPS_HOST", u.Host)
	_, err := InspectIPPPrinter(context.Background(), "ipps://"+u.Host+"/ipp/print")
	if err == nil || !strings.Contains(err.Error(), "307") || redirected.Load() != 0 {
		t.Fatalf("redirect followed with certificate verification disabled: %v", err)
	}
}
