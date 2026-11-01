package utils

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type PrinterTestOptions struct {
	Paper    string  `json:"paper"`
	WidthMM  float64 `json:"width_mm"`
	HeightMM float64 `json:"height_mm"`
	Mode     string  `json:"mode"`
}

func (o PrinterTestOptions) Dimensions() (float64, float64, string, error) {
	switch strings.ToUpper(strings.TrimSpace(o.Paper)) {
	case "", "A4":
		return 210, 297, "A4", nil
	case "F4":
		return 210, 330, "F4", nil
	case "CF", "LABEL", "LAINNYA":
		if math.IsNaN(o.WidthMM) || math.IsNaN(o.HeightMM) || math.IsInf(o.WidthMM, 0) || math.IsInf(o.HeightMM, 0) || o.WidthMM < 25 || o.WidthMM > 500 || o.HeightMM < 25 || o.HeightMM > 500 {
			return 0, 0, "", errors.New("lebar dan tinggi kertas harus 25 sampai 500 mm")
		}
		return o.WidthMM, o.HeightMM, strings.ToUpper(o.Paper), nil
	default:
		return 0, 0, "", errors.New("jenis kertas harus A4, F4, CF, Label, atau Lainnya")
	}
}

func GeneratePrinterTestPDF(printerName string, options PrinterTestOptions) ([]byte, error) {
	widthMM, heightMM, paper, err := options.Dimensions()
	if err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	if mode == "" {
		mode = "bw"
	}
	if mode != "bw" && mode != "color" {
		return nil, errors.New("mode cetak harus hitam putih atau warna")
	}
	pageW, pageH := widthMM*72/25.4, heightMM*72/25.4
	mediaW, mediaH := pageW, pageH
	var content strings.Builder
	writeText := func(x, y, size float64, bold bool, value string) {
		font := "F1"
		if bold {
			font = "F2"
		}
		fmt.Fprintf(&content, "BT /%s %.1f Tf %.1f %.1f Td (%s) Tj ET\n", font, size, x, pageH-y, escapePDFText(value))
	}
	writeLine := func(x1, y1, x2, y2 float64) {
		fmt.Fprintf(&content, "0 0 0 RG 0.6 w %.1f %.1f m %.1f %.1f l S\n", x1, pageH-y1, x2, pageH-y2)
	}
	date := indonesianPrintDate(time.Now())
	if paper == "LABEL" || pageW < 420 || pageH < 710 {
		// shortcut: small media uses a compact page; full-size font samples need A4/F4/CF.
		scale := math.Min(1, math.Min(pageW/283, pageH/142))
		if scale < 1 {
			fmt.Fprintf(&content, "q %.4f 0 0 %.4f 0 0 cm\n", scale, scale)
			pageW, pageH = pageW/scale, pageH/scale
		}
		margin := math.Max(8, pageW*0.06)
		writeText(margin, 20, 11, true, "TEST PRINT PRINTER")
		writeLine(margin, 26, pageW-margin, 26)
		writeText(margin, 40, 8, true, shortenPrinterLabel(printerName, pageW))
		writeText(margin, 53, 7, false, date+"  |  "+paper)
		writeText(margin, 68, 8, false, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		writeText(margin, 81, 8, false, "abcdefghijklmnopqrstuvwxyz")
		writeText(margin, 94, 8, false, "0123456789")
		writeLine(margin, 102, pageW-margin, 102)
		writeText(margin, 116, 7, true, "*** AKHIR TEST PRINT ***")
		if scale < 1 {
			content.WriteString("Q\n")
		}
	} else {
		margin := 35.0
		writeText(margin, 48, 18, true, "TEST PRINT PRINTER")
		writeText(margin, 66, 10, false, "Pengujian Kualitas Hasil Cetak")
		writeLine(margin, 76, pageW-margin, 76)
		writeText(margin, 97, 11, true, "Informasi Pengujian")
		writeText(margin, 115, 9, false, "Nama Printer: "+printerName)
		writeText(margin, 130, 9, false, "Tanggal: "+date)
		writeText(margin, 145, 9, false, fmt.Sprintf("Jenis Kertas: %s (%.0f x %.0f mm)", paper, widthMM, heightMM))
		modeLabel := "Hitam Putih"
		if mode == "color" {
			modeLabel = "Warna"
		}
		writeText(margin, 160, 9, false, "Mode Cetak: "+modeLabel)
		colors := [][3]float64{{0, 0, 0}, {0.55, 0.55, 0.55}, {0.85, 0.85, 0.85}}
		if mode == "color" {
			colors = [][3]float64{{0.86, 0.2, 0.2}, {0.18, 0.62, 0.3}, {0.2, 0.38, 0.83}}
		}
		for i, color := range colors {
			fmt.Fprintf(&content, "%.2f %.2f %.2f rg %.1f %.1f 30 6 re f\n", color[0], color[1], color[2], margin+float64(i)*36, pageH-174)
		}
		content.WriteString("0 0 0 rg\n")
		writeText(margin, 185, 11, true, "Pengujian Karakter")
		writeText(margin, 202, 10, false, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		writeText(margin, 217, 10, false, "abcdefghijklmnopqrstuvwxyz")
		writeText(margin, 232, 10, false, "0123456789")
		writeText(margin, 247, 10, false, "!@#$%^&*()_+-=[]{};:'\",.<>?/")
		writeText(margin, 272, 11, true, "Pengujian Ukuran Font")
		writeText(margin, 288, 8, false, "Font 8 pt — Teks kecil untuk catatan dan keterangan dokumen.")
		writeText(margin, 305, 10, false, "Font 10 pt — Teks standar dokumen administrasi.")
		writeText(margin, 323, 12, false, "Font 12 pt — Teks normal untuk surat dan laporan.")
		writeText(margin, 347, 16, true, "Font 16 pt — Judul Dokumen")
		writeText(margin, 378, 24, true, "Font 24 pt")
		writeText(margin, 405, 11, true, "Pengujian Garis")
		writeLine(margin, 416, pageW-margin, 416)
		writeLine(margin, 424, pageW-margin, 424)
		writeText(margin, 449, 11, true, "Pengujian Format Angka")
		left, right := margin, pageW-margin
		mid := pageW * 0.61
		for i := 0; i <= 4; i++ {
			writeLine(left, 460+float64(i)*22, right, 460+float64(i)*22)
		}
		writeLine(left, 460, left, 548)
		writeLine(mid, 460, mid, 548)
		writeLine(right, 460, right, 548)
		rows := [][2]string{{"Keterangan", "Nilai"}, {"Total Transaksi", "Rp1.250.000"}, {"Pajak (11%)", "Rp137.500"}, {"Total Pembayaran", "Rp1.387.500"}}
		for i, row := range rows {
			y := 475 + float64(i)*22
			writeText(left+8, y, 9, i == 0 || i == 3, row[0])
			writeText(mid+8, y, 9, i == 0 || i == 3, row[1])
		}
		writeText(margin, 600, 11, true, "Pengujian Bahasa Indonesia")
		writeText(margin, 618, 9, false, "Selamat pagi. Pengujian pencetakan dokumen meliputi huruf, angka,")
		writeText(margin, 632, 9, false, "tanda baca, dan simbol. Pastikan seluruh teks tercetak dengan jelas,")
		writeText(margin, 646, 9, false, "tidak terpotong, dan tidak bergaris.")
		writeText(margin, 680, 10, true, "*** AKHIR TEST PRINT ***")
	}
	return assembleTestPDF(mediaW, mediaH, []byte(content.String())), nil
}

func indonesianPrintDate(t time.Time) string {
	months := [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	location, err := time.LoadLocation("Asia/Jakarta")
	if err == nil {
		t = t.In(location)
	}
	return fmt.Sprintf("%d %s %d", t.Day(), months[t.Month()-1], t.Year())
}

func shortenPrinterLabel(name string, width float64) string {
	limit := int((width - 16) / 4.5)
	runes := []rune(name)
	if len(runes) <= limit {
		return name
	}
	if limit < 4 {
		return string(runes[:3])
	}
	return string(runes[:limit-3]) + "..."
}

func escapePDFText(value string) string {
	var text strings.Builder
	for _, r := range value {
		switch r {
		case '\\', '(', ')':
			text.WriteByte('\\')
			text.WriteRune(r)
		case '—':
			text.WriteString("\\227")
		default:
			if r < 128 {
				text.WriteRune(r)
			} else {
				text.WriteByte('?')
			}
		}
	}
	return text.String()
}

func assembleTestPDF(width, height float64, commands []byte) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 4 0 R /F2 5 0 R >> >> /Contents 6 0 R >>", width, height),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(commands), commands),
	}
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = pdf.Len()
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return pdf.Bytes()
}
