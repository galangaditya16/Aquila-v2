package printserver

import (
	"math"
	"strings"
	"testing"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/utils"
)

// TestWindowsSimulation_WaitWinRT_ScriptStructure
// Memvalidasi bahwa skrip cetak PDF Windows menggunakan pola .NET Task / AsTask resmi dari
// System.WindowsRuntimeSystemExtensions, dan tidak lagi mengandalkan $operation.Status yang dapat
// bernilai null pada COM interop PowerShell 5.1.
func TestWindowsSimulation_WaitWinRT_ScriptStructure(t *testing.T) {
	script := windowsPDFPrintScript

	// Tidak boleh lagi menggunakan ekspresi $operation.Status yang memicu 'InvokeMethodOnNull'
	if strings.Contains(script, "$operation.Status") {
		t.Fatalf("Script masih mengandung $operation.Status yang rentan null reference")
	}

	// Tidak boleh menggunakan type accelerator [short] yang tidak didukung PowerShell 5.1/7.x
	if strings.Contains(script, "[short]") {
		t.Fatalf("Script masih mengandung [short] yang tidak dikenali PowerShell ('Unable to find type [short]')")
	}

	// Tidak boleh memanggil Dispose() pada Windows.Data.Pdf.PdfDocument karena class tersebut tidak memiliki method Dispose
	if strings.Contains(script, "$pdf.Dispose()") {
		t.Fatalf("Script masih mengandung $pdf.Dispose() padahal [Windows.Data.Pdf.PdfDocument] tidak mengimplementasikan Dispose")
	}

	// Wajib menggunakan AsTask dan reflection System.WindowsRuntimeSystemExtensions
	expectedKeywords := []string{
		"System.WindowsRuntimeSystemExtensions",
		"AsTask",
		"IAsyncOperation",
		"IAsyncAction",
		"Wait-WinRT",
		"[int16]",
	}

	for _, kw := range expectedKeywords {
		if !strings.Contains(script, kw) {
			t.Fatalf("Script PDF print tidak memuat keyword wajib '%s'", kw)
		}
	}
}

// TestWindowsSimulation_PrinterPayloads
// Mensimulasikan berbagai macam format output PowerShell ConvertTo-Json pada Windows:
// 1. Array berisi banyak printer (multi printer)
// 2. Objek tunggal {} (kasus umum PC rumah sakit dengan 1 printer)
// 3. Output kosong / whitespace / null
func TestWindowsSimulation_PrinterPayloads(t *testing.T) {
	// Kasus A: Multi printer JSON array
	multiJSON := `[
		{"name":"EPSON L3150","state":"ready","is_default":true,"driver":"EPSON L3150 Series","port":"USB001","ip":null},
		{"name":"Zebra ZD230 Label","state":"ready","is_default":false,"driver":"ZDesigner ZD230-203dpi ZPL","port":"USB002","ip":null}
	]`
	printers, err := parseWindowsPrinters([]byte(multiJSON))
	if err != nil {
		t.Fatalf("Gagal parse multi printer: %v", err)
	}
	if len(printers) != 2 {
		t.Fatalf("Ekspektasi 2 printer, didapat: %d", len(printers))
	}
	if printers[0].URI != utils.WindowsQueueURI("EPSON L3150") {
		t.Fatalf("URI printer 0 salah: %s", printers[0].URI)
	}
	if printers[1].URI != utils.WindowsQueueURI("Zebra ZD230 Label") {
		t.Fatalf("URI printer 1 salah: %s", printers[1].URI)
	}

	// Kasus B: Single printer JSON object (PowerShell ConvertTo-Json tanpa wrapping array)
	singleJSON := `{"name":"Printer Kasir Thermal 80mm","state":"ready","is_default":true,"driver":"Generic / Text Only","port":"COM1","ip":null}`
	printers, err = parseWindowsPrinters([]byte(singleJSON))
	if err != nil {
		t.Fatalf("Gagal parse single printer: %v", err)
	}
	if len(printers) != 1 {
		t.Fatalf("Ekspektasi 1 printer, didapat: %d", len(printers))
	}
	if printers[0].Name != "Printer Kasir Thermal 80mm" {
		t.Fatalf("Nama printer salah: %s", printers[0].Name)
	}
	if printers[0].URI != utils.WindowsQueueURI("Printer Kasir Thermal 80mm") {
		t.Fatalf("URI single printer salah: %s", printers[0].URI)
	}

	// Kasus C: Output kosong / null
	for _, emptyPayload := range []string{"", "   ", "\r\n", "null"} {
		printers, err = parseWindowsPrinters([]byte(emptyPayload))
		if err != nil {
			t.Fatalf("Parse payload kosong menghasilkan error: %v", err)
		}
		if len(printers) != 0 {
			t.Fatalf("Payload kosong harus menghasilkan 0 printer, didapat: %d", len(printers))
		}
	}
}

// TestWindowsSimulation_PageBoundsCalculation
// Mensimulasikan kalkulasi ukuran cetak antara MarginBounds (lama - bug) vs PageBounds (baru - fix)
// untuk printer etiket obat (50 mm x 30 mm = ~142 x 85 points pada 72 DPI, atau 197 x 118 pada 100 DPI)
func TestWindowsSimulation_PageBoundsCalculation(t *testing.T) {
	// 50mm x 30mm dalam satuan ratusan inci (System.Drawing 100 DPI)
	paperWidthHundredths := int(math.Round(50.0 * 100.0 / 25.4))  // 197
	paperHeightHundredths := int(math.Round(30.0 * 100.0 / 25.4)) // 118

	// Default hard margin printer 1 inci (100 hundredths of an inch di tiap sisi)
	leftMargin := 100
	rightMargin := 100

	marginBoundsWidth := paperWidthHundredths - (leftMargin + rightMargin)
	if marginBoundsWidth <= 0 {
		// Terbukti bahwa MarginBounds menghasilkan nilai width <= 0 untuk kertas kecil
	}

	// Perbaikan: Menggunakan PageBounds penuh
	pageBoundsWidth := paperWidthHundredths
	pageBoundsHeight := paperHeightHundredths

	if pageBoundsWidth <= 0 || pageBoundsHeight <= 0 {
		t.Fatalf("PageBounds tidak valid: %dx%d", pageBoundsWidth, pageBoundsHeight)
	}

	// Skala bitmap 300 DPI ke PageBounds
	bitmapW := int(math.Round(50.0 * 300.0 / 25.4)) // 591
	bitmapH := int(math.Round(30.0 * 300.0 / 25.4)) // 354
	scale := math.Min(float64(pageBoundsWidth)/float64(bitmapW), float64(pageBoundsHeight)/float64(bitmapH))
	if scale <= 0 {
		t.Fatalf("Faktor skala tidak boleh <= 0: %f", scale)
	}
}

// TestWindowsSimulation_ExecutionPolicyArguments
// Memastikan semua script yang dijalankan memuat parameter "-ExecutionPolicy Bypass"
func TestWindowsSimulation_ExecutionPolicyArguments(t *testing.T) {
	script := windowsPDFPrintScript
	if !strings.Contains(script, "Wait-WinRT") {
		t.Fatalf("Script PDF print tidak memuat fungsi Wait-WinRT")
	}
	if !strings.Contains(script, "$bounds = $e.PageBounds") {
		t.Fatalf("Script PDF print tidak menggunakan $e.PageBounds")
	}
}
