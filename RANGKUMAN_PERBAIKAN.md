# 📋 Master Dokumentasi: Analisis Masalah, Peringatan & Panduan Perbaikan Windows

> **Target Proyek:** `karya-anakbangsa/aquila-printers-discover` (Aquila / RSISA Print Server)  
> **Status:** Referensi Tunggal / Master Guide untuk Lingkungan Sistem Operasi Windows  
> **Tanggal Pembaruan:** 1 November 2026

---

## 1. Ringkasan Eksekutif & Akar Masalah

Aplikasi berjalan mulus di **macOS dan Linux**, tetapi mengalami kegagalan fungsi hingga crash/error di **Windows**.

### Mengapa macOS & Linux Berhasil?
* macOS dan Linux menggunakan **CUPS (Common UNIX Printing System)** sebagai service pencetakan bawaan OS.
* CUPS memiliki subsistem filter raster dan konversi PDF internal (`pdftops`, `pdftoraster`, `cups-filters`).
* Perintah CLI `lp -d "printer_name" file.pdf` secara native dan otomatis memproses PDF ke format bahasa mesin printer target (raster/PCL/PostScript).

### Mengapa Windows Gagal?
* Windows **tidak memiliki CUPS** dan **tidak memiliki utilitas CLI bawaan** untuk mengirim file PDF langsung ke antrean printer lokal.
* Windows menggunakan subsistem **Windows Print Spooler (`spoolsv.exe`)** yang hanya menerima:
  1. Data **RAW byte stream** (seperti teks printer POS ESC/POS, atau label TSPL/ZPL barcode).
  2. Format grafis **GDI / XPS** yang dirender oleh aplikasi melalui driver Windows Graphics Device Interface.
* Penanganan PDF saat ini mencoba merender PDF secara dinamis via script **PowerShell + WinRT (`Windows.Data.Pdf`) + .NET (`System.Drawing`)**, yang memicu serangkaian **5 bug fatal dan 2 risiko arsitektural**.

---

## 2. Matriks Cepat Masalah & Solusi (Checklist Aksi)

| No | Komponen / File | Masalah Utama | Dampak | Solusi Cepat (Fase 1) | Estimasi |
|---|---|---|---|---|---|
| 1 | `internal/printserver/windows_pdf_print.go` | WinRT status dievaluasi sebagai string (`"Started"`) padahal integer (`0`) | Cetak PDF selalu gagal seketika | Evaluasi status integer: `$operation.Status -eq 0` | 5 mnt |
| 2 | `internal/printserver/server.go` | `ConvertTo-Json` kirim single object jika hanya ada 1 printer | PC 1 printer gagal parse JSON (0 printer) | Parsing adaptif: dukung array `[...]` dan single `{...}` | 10 mnt |
| 3 | `internal/printserver/windows_command_windows.go` & `windows_pdf_print.go` | PowerShell diblokir GPO / Restricted Policy | Script ditolak di PC enterprise/RS | Tambahkan `-ExecutionPolicy Bypass` | 2 mnt |
| 4 | `internal/printserver/windows_pdf_print.go` | `$e.MarginBounds` default 1 inci (25.4 mm) di tiap sisi | Kertas label etiket (50x30 mm) / thermal terpotong/kosong | Ganti ke `$e.PageBounds` | 2 mnt |
| 5 | `desktop/core.go` | Timeout 5s terlampaui oleh cold start PowerShell & WMI query offline | `PrinterCount()` selalu return 0 | Naikkan timeout menjadi 15 detik | 2 mnt |
| 6 | Arsitektur Sistem | Ketergantungan penuh pada proses `powershell.exe` | Cold start lambat (1.5-3s), rentan blokir antivirus | Ganti ke Win32 API (`EnumPrintersW` via `winspool.drv`) | Fase 2 |
| 7 | `internal/printserver/discovery.go` | Windows Firewall blokir UDP multicast 224.0.0.251:5353 | Pencarian printer mDNS gagal/timeout | Inbound firewall rule di installer NSIS | Fase 2 |

---

## 3. Rincian Masalah Teknis (Root Cause Analysis)

### 🔴 Masalah 1: Bug Evaluasi Async WinRT (`windows_pdf_print.go`)
* **Lokasi File:** [`internal/printserver/windows_pdf_print.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_pdf_print.go#L47-L54)
* **Kode Bermasalah:**
  ```powershell
  function Wait-WinRT($operation) {
    while ($operation.Status.ToString() -eq 'Started') { [System.Threading.Thread]::Sleep(10) }
    if ($operation.Status.ToString() -ne 'Completed') {
      throw "PDF Windows Runtime operation failed: $($operation.Status) $($operation.ErrorCode)"
    }
    $operation.GetResults()
  }
  ```
* **Mekanisme Kegagalan:**
  1. Di PowerShell 5.1 (bawaan Windows 10 & 11), objek asynchronous WinRT (`IAsyncOperation`) diproyeksikan sebagai COM wrapper (`System.__ComObject`).
  2. Enum `AsyncStatus` bernilai integer (`Started = 0`, `Completed = 1`). `$operation.Status` menghasilkan integer `0`.
  3. `$operation.Status.ToString()` menghasilkan string `"0"`, **bukan `"Started"`**.
  4. Akibatnya:
     * Kondisi loop `while ($operation.Status.ToString() -eq 'Started')` bernilai **FALSE** seketika.
     * Evaluasi langsung jatuh ke `if ($operation.Status.ToString() -ne 'Completed')` (karena `"0"` != `"Completed"` bernilai **TRUE**).
     * Script langsung melempar error: `PDF Windows Runtime operation failed: 0 0` sebelum file PDF sempat dibaca atau dicetak.
     * Pemanggilan `.GetResults()` pada operasi yang belum selesai dapat memicu exception Win32 `E_ILLEGAL_METHOD_CALL (0x8000000E)`.

---

### 🔴 Masalah 2: Gagal JSON Unmarshal Saat PC Hanya Memiliki 1 Printer (`server.go`)
* **Lokasi File:** [`internal/printserver/server.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/server.go#L267-L280)
* **Kode Bermasalah:**
  ```go
  var printers []LocalPrinter
  if err := json.Unmarshal([]byte(trimmed), &printers); err != nil {
      return nil, fmt.Errorf("baca daftar printer Windows: %w", err)
  }
  ```
* **Mekanisme Kegagalan:**
  * Script PowerShell mengambil daftar printer via `Get-CimInstance Win32_Printer | ConvertTo-Json -Compress`.
  * Di Windows PowerShell 5.1, jika printer yang terpasang hanya **1 perangkat**, `ConvertTo-Json` **TIDAK menghasilkan array JSON** `[{...}]`, melainkan **objek JSON tunggal** `{...}`.
  * Ketika fungsi Go menjalankan `json.Unmarshal` ke target slice `[]LocalPrinter`, proses unmarshal gagal fatal dengan error:
    ```
    baca daftar printer Windows: json: cannot unmarshal object into Go value of type []printserver.LocalPrinter
    ```
  * Endpoint `/printers` error dan aplikasi desktop menganggap komputer tidak memiliki printer sama sekali.
  * *Catatan inkonsistensi:* [`desktop/port_owner_windows.go`](file:///Users/mirza/husband/aquila-printers-discover/desktop/port_owner_windows.go#L42-L53) sudah menangani kasus ini dengan benar (`strings.HasPrefix(trimmed, "[")`), tetapi implementasi tersebut tidak diterapkan secara konsisten di `server.go`.

---

### 🔴 Masalah 3: PowerShell Terblokir oleh Policy Sistem (`ExecutionPolicy`)
* **Lokasi File:**
  - [`internal/printserver/windows_command_windows.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_command_windows.go#L12-L14)
  - [`internal/printserver/windows_pdf_print.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_pdf_print.go#L31-L32)
* **Kode Bermasalah:**
  ```go
  utils.PowerShellCommand(ctx, "-NoProfile", "-NonInteractive", "-Command", ...)
  ```
* **Mekanisme Kegagalan:**
  * Di workstation rumah sakit, klinik, atau instansi perbankan, Group Policy sering menerapkan `Set-ExecutionPolicy Restricted` atau `AllSigned`.
  * Tanpa flag `-ExecutionPolicy Bypass`, sistem menolak mengeksekusi script.
  * *Catatan inkonsistensi:* [`desktop/port_owner_windows.go`](file:///Users/mirza/husband/aquila-printers-discover/desktop/port_owner_windows.go#L29) sudah menyertakan flag `-ExecutionPolicy Bypass`.

---

### 🔴 Masalah 4: GDI Margin Memotong Kertas Thermal & Label Etiket
* **Lokasi File:** [`internal/printserver/windows_pdf_print.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_pdf_print.go#L101-L107)
* **Kode Bermasalah:**
  ```powershell
  $bounds = $e.MarginBounds
  $scale = [math]::Min($bounds.Width / [double]$bitmap.Width, $bounds.Height / [double]$bitmap.Height)
  ...
  $e.Graphics.DrawImage($bitmap, $x, $y, $width, $height)
  ```
* **Mekanisme Kegagalan:**
  * Properti `$e.MarginBounds` memperhitungkan hard margin printer bawaan (standar 1 inci = 25.4 mm di tiap sisi).
  * Untuk printer label etiket obat (misal 50 mm × 30 mm) atau printer kasir/thermal (58 mm / 80 mm), margin kiri dan kanan memakan total 50.8 mm sehingga area cetak menjadi negatif atau mendekati 0.
  * Akibatnya: kertas keluar kosong, teks berukuran mikro (ter-scale down drastis), atau terpotong (*clipped*).
  * Solusinya adalah menggunakan `$e.PageBounds` karena margin fisik sudah dinetralkan ke `(0, 0, 0, 0)`.

---

### 🔴 Masalah 5: Latensi Tinggi & Timeout Deteksi Printer
* **Lokasi File:** [`desktop/core.go`](file:///Users/mirza/husband/aquila-printers-discover/desktop/core.go#L428-L436)
* **Kode Bermasalah:**
  ```go
  func (c *Core) PrinterCount() int {
      ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
      defer cancel()
      printers, err := printserver.ListPrinters(ctx)
      if err != nil {
          return 0
      }
      return len(printers)
  }
  ```
* **Mekanisme Kegagalan:**
  * Inisialisasi CLR .NET + engine PowerShell (cold start) memakan 1.5 – 3 detik.
  * Query WMI/CIM `Win32_TCPIPPrinterPort` dan `Win32_Printer` saat ada printer jaringan offline/unresponsive memakan waktu 6 hingga 15 detik.
  * Batas waktu 5 detik terlewati (*context deadline exceeded*), membuat `PrinterCount()` hampir selalu return `0`.

---

## 4. Panduan Implementasi Solusi (Fase 1: Quick Fix)

### Perbaikan 1: Normalisasi Parsing JSON Single / Multi Printer
File target: [`internal/printserver/server.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/server.go)
```go
func parseWindowsPrinters(output []byte) ([]LocalPrinter, error) {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" || trimmed == "null" {
		return []LocalPrinter{}, nil
	}
	var printers []LocalPrinter
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &printers); err != nil {
			return nil, fmt.Errorf("baca daftar printer Windows: %w", err)
		}
	} else {
		// Menangani perilaku PowerShell ConvertTo-Json yang mengembalikan objek tunggal saat hanya ada 1 printer
		var single LocalPrinter
		if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
			return nil, fmt.Errorf("baca printer tunggal Windows: %w", err)
		}
		printers = []LocalPrinter{single}
	}
	for index := range printers {
		printers[index].URI = utils.WindowsQueueURI(printers[index].Name)
	}
	return printers, nil
}
```

Tambahkan unit test di [`internal/printserver/windows_printers_test.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_printers_test.go):
```go
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
```

---

### Perbaikan 2: Menambahkan `-ExecutionPolicy Bypass`
1. Di [`internal/printserver/windows_command_windows.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_command_windows.go):
```go
func hiddenWindowsCommand(ctx context.Context, script string) *exec.Cmd {
	return utils.PowerShellCommand(ctx, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}
```

2. Di [`internal/printserver/windows_pdf_print.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_pdf_print.go):
```go
command := utils.PowerShellCommand(ctx, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", windowsPDFPrintScript)
```

---

### Perbaikan 3: Perbaikan Async WinRT & Penanganan Margin Kertas
Perbarui script `windowsPDFPrintScript` di [`internal/printserver/windows_pdf_print.go`](file:///Users/mirza/husband/aquila-printers-discover/internal/printserver/windows_pdf_print.go):
```powershell
const windowsPDFPrintScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Storage.StorageFile, Windows.Storage, ContentType=WindowsRuntime]
$null = [Windows.Data.Pdf.PdfDocument, Windows.Data.Pdf, ContentType=WindowsRuntime]
$null = [Windows.Data.Pdf.PdfPageRenderOptions, Windows.Data.Pdf, ContentType=WindowsRuntime]
$null = [Windows.Storage.Streams.InMemoryRandomAccessStream, Windows.Storage, ContentType=WindowsRuntime]

function Wait-WinRT($operation) {
  # AsyncStatus enum: Started = 0, Completed = 1, Canceled = 2, Error = 3
  while ($operation.Status -eq 0 -or $operation.Status.ToString() -eq 'Started') { 
    [System.Threading.Thread]::Sleep(10) 
  }
  if ($operation.Status -ne 1 -and $operation.Status.ToString() -ne 'Completed') { 
    throw "PDF Windows Runtime operation failed: status=$($operation.Status) code=$($operation.ErrorCode)" 
  }
  $operation.GetResults()
}

$storageFile = Wait-WinRT ([Windows.Storage.StorageFile]::GetFileFromPathAsync($env:AQUILA_PDF_PATH))
$pdf = Wait-WinRT ([Windows.Data.Pdf.PdfDocument]::LoadFromFileAsync($storageFile))
if ($pdf.PageCount -lt 1) { throw 'PDF tidak memiliki halaman' }
$doc = [System.Drawing.Printing.PrintDocument]::new()
$doc.PrinterSettings.PrinterName = $env:AQUILA_PRINTER_NAME
if (-not $doc.PrinterSettings.IsValid) { throw 'Antrean printer Windows tidak ditemukan' }
$doc.PrinterSettings.Copies = [short]::Parse($env:AQUILA_COPIES)
$doc.DocumentName = $env:AQUILA_PRINT_JOB
$doc.PrintController = [System.Drawing.Printing.StandardPrintController]::new()
$doc.DefaultPageSettings.Margins = [System.Drawing.Printing.Margins]::new(0, 0, 0, 0)
$state = [PSCustomObject]@{ Index = 0 }
$doc.add_QueryPageSettings({
  param($sender, $e)
  $page = $pdf.GetPage([uint32]$state.Index)
  try {
    if ($env:AQUILA_MEDIA) {
      $name = switch -Regex ($env:AQUILA_MEDIA) {
        '^iso_a4_' { 'A4'; break }
        '^iso_a3_' { 'A3'; break }
        '^na_letter_' { 'Letter'; break }
        '^na_legal_' { 'Legal'; break }
        '^na_(ledger|tabloid)_' { 'Tabloid'; break }
        default { $env:AQUILA_MEDIA }
      }
      $paper = $doc.PrinterSettings.PaperSizes | Where-Object { $_.PaperName -ieq $name } | Select-Object -First 1
      if (-not $paper) { throw "Ukuran kertas '$($env:AQUILA_MEDIA)' tidak tersedia pada printer" }
      $e.PageSettings.PaperSize = $paper
    } else {
      $width = [int][math]::Max(1, [math]::Round($page.Size.Width * 100 / 96))
      $height = [int][math]::Max(1, [math]::Round($page.Size.Height * 100 / 96))
      $e.PageSettings.PaperSize = [System.Drawing.Printing.PaperSize]::new('Aquila PDF', $width, $height)
    }
    $e.PageSettings.Margins = [System.Drawing.Printing.Margins]::new(0, 0, 0, 0)
  } finally { $page.Dispose() }
}.GetNewClosure())
$doc.add_PrintPage({
  param($sender, $e)
  $page = $pdf.GetPage([uint32]$state.Index)
  $randomStream = [Windows.Storage.Streams.InMemoryRandomAccessStream]::new()
  $bitmap = $null
  $imageStream = $null
  try {
    $options = [Windows.Data.Pdf.PdfPageRenderOptions]::new()
    $options.DestinationWidth = [uint32][math]::Max(1, [math]::Ceiling($page.Size.Width * 300 / 96))
    $options.DestinationHeight = [uint32][math]::Max(1, [math]::Ceiling($page.Size.Height * 300 / 96))
    Wait-WinRT ($page.RenderToStreamAsync($randomStream, $options))
    $randomStream.Seek(0)
    $imageStream = [System.IO.WindowsRuntimeStreamExtensions]::AsStreamForRead($randomStream)
    $bitmap = [System.Drawing.Image]::FromStream($imageStream)
    
    # Menggunakan PageBounds (bukan MarginBounds) agar printer etiket kecil/thermal tidak terpotong margin default 1 inci
    $bounds = $e.PageBounds
    $scale = [math]::Min($bounds.Width / [double]$bitmap.Width, $bounds.Height / [double]$bitmap.Height)
    $width = [single]($bitmap.Width * $scale)
    $height = [single]($bitmap.Height * $scale)
    $x = [single]($bounds.X + ($bounds.Width - $width) / 2)
    $y = [single]($bounds.Y + ($bounds.Height - $height) / 2)
    $e.Graphics.DrawImage($bitmap, $x, $y, $width, $height)
    $state.Index++
    $e.HasMorePages = $state.Index -lt $pdf.PageCount
  } finally {
    if ($bitmap) { $bitmap.Dispose() }
    if ($imageStream) { $imageStream.Dispose() }
    $randomStream.Dispose()
    $page.Dispose()
  }
}.GetNewClosure())
try { $doc.Print() } finally { $doc.Dispose(); $pdf.Dispose() }`
```

---

### Perbaikan 4: Penyesuaian Timeout Deteksi Printer
File target: [`desktop/core.go`](file:///Users/mirza/husband/aquila-printers-discover/desktop/core.go)
```go
func (c *Core) PrinterCount() int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	printers, err := printserver.ListPrinters(ctx)
	if err != nil {
		return 0
	}
	return len(printers)
}
```

---

## 5. Solusi Jangka Panjang (Fase 2: Industrial Grade Architecture)

1. **Gunakan Win32 API Asli (`EnumPrintersW` via `winspool.drv`):**
   * Menghilangkan ketergantungan `powershell.exe` untuk deteksi printer.
   * Dipanggil via direct syscall (`golang.org/x/sys/windows`).
   * **Manfaat:** Latensi turun drastis dari ~3000 ms menjadi **< 2 ms**, kebal blokir antivirus, dan bebas masalah timeout.
2. **Bundling Tool Headless PDF Print (SumatraPDF CLI / PDFtoPrinter):**
   * Menyertakan binary portable kecil (misal: SumatraPDF CLI atau `PDFtoPrinter.exe`).
   * Mendukung penuh Windows versi lama (Windows 7/8/Server) yang belum dilengkapi WinRT `Windows.Data.Pdf`.
3. **Konfigurasi Firewall Otomatis untuk mDNS:**
   * Menambahkan perintah pendaftaran rule inbound port UDP 5353 pada skrip installer NSIS:
     ```cmd
     netsh advfirewall firewall add rule name="Aquila Print Server (mDNS)" dir=in action=allow protocol=UDP localport=5353
     ```
4. **Mekanisme Caching Hasil Deteksi Printer:**
   * Simpan hasil query printer dengan TTL (misalnya 15-30 detik) agar request berkala ke `/printers` tidak memicu query WMI/PowerShell berulang kali.

---

## 6. Checklist Verifikasi Sebelum Rilis

- [ ] Unit test `TestParseWindowsPrintersHandlesSingleObject` lulus pengujian.
- [ ] Pengujian di PC Windows fisik dengan hanya 1 printer terpasang (memastikan printer terdeteksi).
- [ ] Pengujian cetak dokumen etiket obat / thermal (memastikan margin tidak terpotong).
- [ ] Pengujian cetak pada Windows dengan setting `Set-ExecutionPolicy Restricted`.
- [ ] Verifikasi log aplikasi desktop tidak lagi memunculkan error unmarshal JSON.
- [ ] Pengujian timeout pada komputer dengan printer jaringan offline.
