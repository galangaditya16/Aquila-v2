package printserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRawTCPPDFIsRejectedBeforeSending(t *testing.T) {
	server := New(Config{}, func(level, message string) {})
	app := server.App()

	printRequest := httptest.NewRequest(http.MethodPost, "/print/billing_produksi?module_code=barang_produksi", strings.NewReader("%PDF-1.4\n%%EOF"))
	printRequest.Header.Set("X-RSISA-Printer-URI", "tcp://127.0.0.1:9100")
	testRequest := httptest.NewRequest(http.MethodPost, "/printers/test", strings.NewReader(`{"name":"EPSON","uri":"tcp://127.0.0.1:9100","paper":"A4","mode":"bw"}`))
	testRequest.Header.Set("Content-Type", "application/json")

	for _, request := range []*http.Request{printRequest, testRequest} {
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != fiber.StatusUnprocessableEntity {
			t.Fatalf("%s: got status %d, want 422", request.URL.Path, response.StatusCode)
		}
	}
}
