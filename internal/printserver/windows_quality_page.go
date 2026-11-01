package printserver

import (
	"context"
	"fmt"
	"os"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/utils"
)

func sendWindowsQualityTest(ctx context.Context, printerName string, options utils.PrinterTestOptions) error {
	widthMM, heightMM, paper, err := options.Dimensions()
	if err != nil {
		return err
	}
	mode := options.Mode
	if mode == "" {
		mode = "bw"
	}
	script := `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$widthMM = [double]::Parse($env:RSISA_TEST_WIDTH, [System.Globalization.CultureInfo]::InvariantCulture)
$heightMM = [double]::Parse($env:RSISA_TEST_HEIGHT, [System.Globalization.CultureInfo]::InvariantCulture)
$doc = [System.Drawing.Printing.PrintDocument]::new()
$doc.PrinterSettings.PrinterName = $env:RSISA_PRINTER_NAME
if (-not $doc.PrinterSettings.IsValid) { throw 'Printer tidak ditemukan pada Windows' }
$doc.DefaultPageSettings.PaperSize = [System.Drawing.Printing.PaperSize]::new('RSISA Test', [int][math]::Round($widthMM * 100 / 25.4), [int][math]::Round($heightMM * 100 / 25.4))
$doc.DefaultPageSettings.Color = ($env:RSISA_TEST_MODE -eq 'color')
$doc.add_PrintPage({
  param($sender, $e)
  $g = $e.Graphics
  $g.PageUnit = [System.Drawing.GraphicsUnit]::Point
  $pageW = $widthMM * 72 / 25.4
  $pageH = $heightMM * 72 / 25.4
  $ink = [System.Drawing.Brushes]::Black
  $draw = {
    param([string]$value, [double]$x, [double]$y, [double]$size, [bool]$bold)
    $style = if ($bold) { [System.Drawing.FontStyle]::Bold } else { [System.Drawing.FontStyle]::Regular }
    $font = [System.Drawing.Font]::new('Arial', [single]$size, $style)
    try { $g.DrawString($value, $font, $ink, [single]$x, [single]$y) } finally { $font.Dispose() }
  }.GetNewClosure()
  $line = {
    param([double]$x1, [double]$y1, [double]$x2, [double]$y2)
    $g.DrawLine([System.Drawing.Pens]::Black, [single]$x1, [single]$y1, [single]$x2, [single]$y2)
  }.GetNewClosure()
  $date = (Get-Date).ToString('d MMMM yyyy', [System.Globalization.CultureInfo]::GetCultureInfo('id-ID'))
  if ($env:RSISA_TEST_PAPER -eq 'LABEL' -or $pageW -lt 420 -or $pageH -lt 710) {
    $scale = [math]::Min(1, [math]::Min($pageW / 283, $pageH / 142))
    if ($scale -lt 1) {
      $g.ScaleTransform([single]$scale, [single]$scale)
      $pageW /= $scale
      $pageH /= $scale
    }
    $m = [math]::Max(8, $pageW * 0.06)
    & $draw 'TEST PRINT PRINTER' $m 8 11 $true
    & $line $m 26 ($pageW - $m) 26
    & $draw $env:RSISA_PRINTER_NAME $m 29 8 $true
    & $draw ($date + ' | ' + $env:RSISA_TEST_PAPER) $m 43 7 $false
    & $draw 'ABCDEFGHIJKLMNOPQRSTUVWXYZ' $m 57 8 $false
    & $draw 'abcdefghijklmnopqrstuvwxyz' $m 70 8 $false
    & $draw '0123456789' $m 83 8 $false
    & $line $m 102 ($pageW - $m) 102
    & $draw '*** AKHIR TEST PRINT ***' $m 104 7 $true
  } else {
    $m = 35
    $modeLabel = if ($env:RSISA_TEST_MODE -eq 'color') { 'Warna' } else { 'Hitam Putih' }
    & $draw 'TEST PRINT PRINTER' $m 30 18 $true
    & $draw 'Pengujian Kualitas Hasil Cetak' $m 53 10 $false
    & $line $m 76 ($pageW - $m) 76
    & $draw 'Informasi Pengujian' $m 84 11 $true
    & $draw ('Nama Printer: ' + $env:RSISA_PRINTER_NAME) $m 103 9 $false
    & $draw ('Tanggal: ' + $date) $m 118 9 $false
    & $draw ('Jenis Kertas: ' + $env:RSISA_TEST_PAPER) $m 133 9 $false
    & $draw ('Mode Cetak: ' + $modeLabel) $m 148 9 $false
    $swatches = if ($env:RSISA_TEST_MODE -eq 'color') {
      @([System.Drawing.Color]::IndianRed, [System.Drawing.Color]::ForestGreen, [System.Drawing.Color]::RoyalBlue)
    } else {
      @([System.Drawing.Color]::Black, [System.Drawing.Color]::Gray, [System.Drawing.Color]::LightGray)
    }
    for ($i = 0; $i -lt $swatches.Count; $i++) {
      $swatchBrush = [System.Drawing.SolidBrush]::new($swatches[$i])
      try { $g.FillRectangle($swatchBrush, [single]($m + $i * 36), [single]166, [single]30, [single]6) } finally { $swatchBrush.Dispose() }
    }
    & $draw 'Pengujian Karakter' $m 171 11 $true
    & $draw 'ABCDEFGHIJKLMNOPQRSTUVWXYZ' $m 189 10 $false
    & $draw 'abcdefghijklmnopqrstuvwxyz' $m 204 10 $false
    & $draw '0123456789' $m 219 10 $false
    & $draw '!@#$%^&*()_+-=[]{};:''",.<>?/' $m 234 10 $false
    & $draw 'Pengujian Ukuran Font' $m 257 11 $true
    & $draw 'Font 8 pt - Teks kecil untuk catatan dan keterangan dokumen.' $m 275 8 $false
    & $draw 'Font 10 pt - Teks standar dokumen administrasi.' $m 291 10 $false
    & $draw 'Font 12 pt - Teks normal untuk surat dan laporan.' $m 309 12 $false
    & $draw 'Font 16 pt - Judul Dokumen' $m 332 16 $true
    & $draw 'Font 24 pt' $m 359 24 $true
    & $draw 'Pengujian Garis' $m 392 11 $true
    & $line $m 416 ($pageW - $m) 416
    & $line $m 424 ($pageW - $m) 424
    & $draw 'Pengujian Format Angka' $m 436 11 $true
    $mid = $pageW * 0.61
    for ($i = 0; $i -le 4; $i++) { & $line $m (460 + $i * 22) ($pageW - $m) (460 + $i * 22) }
    & $line $m 460 $m 548
    & $line $mid 460 $mid 548
    & $line ($pageW - $m) 460 ($pageW - $m) 548
    $rows = @(@('Keterangan','Nilai'),@('Total Transaksi','Rp1.250.000'),@('Pajak (11%)','Rp137.500'),@('Total Pembayaran','Rp1.387.500'))
    for ($i = 0; $i -lt $rows.Count; $i++) {
      & $draw $rows[$i][0] ($m + 8) (463 + $i * 22) 9 ($i -eq 0 -or $i -eq 3)
      & $draw $rows[$i][1] ($mid + 8) (463 + $i * 22) 9 ($i -eq 0 -or $i -eq 3)
    }
    & $draw 'Pengujian Bahasa Indonesia' $m 587 11 $true
    & $draw 'Selamat pagi. Pengujian pencetakan dokumen meliputi huruf, angka,' $m 605 9 $false
    & $draw 'tanda baca, dan simbol. Pastikan seluruh teks tercetak dengan jelas,' $m 619 9 $false
    & $draw 'tidak terpotong, dan tidak bergaris.' $m 633 9 $false
    & $draw '*** AKHIR TEST PRINT ***' $m 667 10 $true
  }
  $e.HasMorePages = $false
})
try { $doc.Print() } finally { $doc.Dispose() }`
	command := hiddenWindowsCommand(ctx, script)
	command.Env = append(os.Environ(),
		"RSISA_PRINTER_NAME="+printerName,
		"RSISA_TEST_WIDTH="+fmt.Sprintf("%.2f", widthMM),
		"RSISA_TEST_HEIGHT="+fmt.Sprintf("%.2f", heightMM),
		"RSISA_TEST_PAPER="+paper,
		"RSISA_TEST_MODE="+mode,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return commandFailure("cetak halaman tes ke antrean Windows", output, err)
	}
	return nil
}
