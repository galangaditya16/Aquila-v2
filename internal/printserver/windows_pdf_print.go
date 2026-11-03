package printserver

import (
	"context"
	"fmt"
	"os"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/utils"
)

func printPDFToWindowsQueue(ctx context.Context, printerName string, pdf []byte, jobName string, copies int, media string) error {
	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		return fmt.Errorf("dokumen bukan file PDF yang valid")
	}
	if copies < 1 || copies > 20 {
		return fmt.Errorf("jumlah salinan harus antara 1 dan 20")
	}
	file, err := os.CreateTemp("", "aquila-print-*.pdf")
	if err != nil {
		return fmt.Errorf("buat dokumen cetak: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(pdf); err != nil {
		_ = file.Close()
		return fmt.Errorf("tulis dokumen cetak: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("simpan dokumen cetak: %w", err)
	}
	command := utils.PowerShellCommand(ctx, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", windowsPDFPrintScript)
	command.Env = append(os.Environ(), "AQUILA_PDF_PATH="+path, "AQUILA_PRINTER_NAME="+printerName, "AQUILA_PRINT_JOB="+jobName, fmt.Sprintf("AQUILA_COPIES=%d", copies), "AQUILA_MEDIA="+media)
	output, err := command.CombinedOutput()
	if err != nil {
		return commandFailure("cetak PDF melalui driver printer Windows", output, err)
	}
	return nil
}

const windowsPDFPrintScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Storage.StorageFile, Windows.Storage, ContentType=WindowsRuntime]
$null = [Windows.Data.Pdf.PdfDocument, Windows.Data.Pdf, ContentType=WindowsRuntime]
$null = [Windows.Data.Pdf.PdfPageRenderOptions, Windows.Data.Pdf, ContentType=WindowsRuntime]
$null = [Windows.Storage.Streams.InMemoryRandomAccessStream, Windows.Storage, ContentType=WindowsRuntime]
$asTaskGeneric = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
  $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetGenericArguments().Count -eq 1 -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name.StartsWith('IAsyncOperation')
} | Select-Object -First 1
$asTaskAction = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object {
  $_.Name -eq 'AsTask' -and (-not $_.IsGenericMethod) -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncAction'
} | Select-Object -First 1
function Wait-WinRT($operation, [Type]$resultType = $null) {
  if ($null -eq $operation) { throw 'WinRT asynchronous operation bernilai null' }
  if ($resultType) {
    $netTask = $asTaskGeneric.MakeGenericMethod($resultType).Invoke($null, @($operation))
    $netTask.Wait(-1) | Out-Null
    return $netTask.Result
  }
  $netTask = $asTaskAction.Invoke($null, @($operation))
  $netTask.Wait(-1) | Out-Null
}
$pdfPath = [System.IO.Path]::GetFullPath($env:AQUILA_PDF_PATH)
$storageFile = Wait-WinRT ([Windows.Storage.StorageFile]::GetFileFromPathAsync($pdfPath)) ([Windows.Storage.StorageFile])
$pdf = Wait-WinRT ([Windows.Data.Pdf.PdfDocument]::LoadFromFileAsync($storageFile)) ([Windows.Data.Pdf.PdfDocument])
if ($pdf.PageCount -lt 1) { throw 'PDF tidak memiliki halaman' }
$doc = [System.Drawing.Printing.PrintDocument]::new()
$doc.PrinterSettings.PrinterName = $env:AQUILA_PRINTER_NAME
if (-not $doc.PrinterSettings.IsValid) { throw 'Antrean printer Windows tidak ditemukan' }
$doc.PrinterSettings.Copies = [int16]::Parse($env:AQUILA_COPIES)
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
try { $doc.Print() } finally { $doc.Dispose() }`
