package printserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/utils"

	"github.com/gofiber/fiber/v2"
)

const maxPDFBytes = 20 << 20

// EventLog menerima catatan peristiwa layanan untuk ditampilkan di UI desktop.
type EventLog func(level, message string)

// Server membungkus aplikasi Fiber print server beserta konfigurasinya.
// Konfigurasi dapat diperbarui saat berjalan (misalnya daftar origin) tanpa
// perlu me-restart listener.
type Server struct {
	mu     sync.RWMutex
	cfg    Config
	app    *fiber.App
	events EventLog
}

// New membuat server print dengan konfigurasi awal. events boleh nil.
func New(cfg Config, events EventLog) *Server {
	if events == nil {
		events = func(level, message string) {
			log.Printf("%s %s", level, message)
		}
	}
	server := &Server{cfg: cfg, events: events}
	app := fiber.New(fiber.Config{
		BodyLimit:     maxPDFBytes + 1024,
		ReadTimeout:   30 * time.Second,
		WriteTimeout:  60 * time.Second,
		Concurrency:   256 * 1024,
		StrictRouting: true,
	})
	app.Use(server.corsMiddleware)
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "rsisa-print-server"})
	})
	app.Get("/printers", listLocalPrinters)
	app.Get("/printers/discover", discoverNetworkPrinters)
	app.Post("/printers/test", server.testLocalPrinter)
	app.Post("/print/:documentType", server.submitPDF)
	server.app = app
	return server
}

// App mengembalikan aplikasi Fiber yang dipakai (untuk pengujian).
func (s *Server) App() *fiber.App { return s.app }

// Listen menjalankan server pada alamat yang diberikan dan memblokir.
func (s *Server) Listen(addr string) error { return s.app.Listen(addr) }

// Shutdown menghentikan server secara graceful.
func (s *Server) Shutdown(ctx context.Context) error { return s.app.ShutdownWithContext(ctx) }

// Config mengembalikan salinan konfigurasi saat ini.
func (s *Server) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	cfg.AllowedOrigin = append([]string(nil), s.cfg.AllowedOrigin...)
	cfg.Printers = append([]PrinterConfig(nil), s.cfg.Printers...)
	cfg.Routes = append([]RouteConfig(nil), s.cfg.Routes...)
	return cfg
}

// UpdateConfig mengganti konfigurasi yang dipakai saat berjalan; efektif untuk
// request berikutnya tanpa restart.
func (s *Server) UpdateConfig(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return nil
}

func (s *Server) logf(level, format string, args ...any) {
	s.events(level, fmt.Sprintf(format, args...))
}

func (s *Server) submitPDF(c *fiber.Ctx) error {
	cfg := s.Config()
	if len(c.Body()) > maxPDFBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "ukuran PDF maksimal 20 MB")
	}
	pdf := c.Body()
	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "kirim isi file PDF dengan Content-Type application/pdf")
	}
	module := strings.TrimSpace(c.Query("module_code"))
	if module == "" {
		module = "umum"
	}
	docType := strings.TrimSpace(c.Params("documentType"))
	var route RouteConfig
	var printer PrinterConfig
	if rawURI := strings.TrimSpace(c.Get("X-RSISA-Printer-URI")); rawURI != "" {
		uri, err := utils.NormalizePrinterURI(rawURI)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		copies := 1
		if rawCopies := strings.TrimSpace(c.Get("X-RSISA-Copies")); rawCopies != "" {
			copies, err = strconv.Atoi(rawCopies)
			if err != nil || copies < 1 || copies > 20 {
				return fiber.NewError(fiber.StatusBadRequest, "jumlah salinan harus antara 1 dan 20")
			}
		}
		printer = PrinterConfig{Name: strings.TrimSpace(c.Get("X-RSISA-Printer-Name")), URI: uri}
		if printer.Name == "" {
			printer.Name = uri
		}
		route = RouteConfig{Copies: copies, Media: strings.TrimSpace(c.Get("X-RSISA-Media"))}
	} else {
		var found bool
		route, found = findRoute(cfg, module, docType)
		if !found && module != "umum" {
			route, found = findRoute(cfg, "umum", docType)
		}
		if !found {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "printer untuk cetakan ini belum diatur di aplikasi")
		}
		printer, found = findPrinter(cfg, route.Printer)
		if !found {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "printer tujuan tidak ditemukan di konfigurasi print server")
		}
	}
	if utils.IsTCPPrinter(printer.URI) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "TCP 9100 mengirim PDF mentah dan dapat mencetak karakter acak. Pilih antrean printer PC ini melalui IPP (127.0.0.1:631).")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 50*time.Second)
	defer cancel()
	ippJobID, err := submitPrinterPDF(ctx, printer.URI, pdf, docType, "farmasi", route.Copies, route.Media)
	if err != nil {
		s.logf("ERROR", "POST /print/%s gagal ke %s: %v", docType, printer.Name, err)
		var ippErr *utils.IPPSubmissionError
		if errors.As(err, &ippErr) {
			return fiber.NewError(fiber.StatusBadGateway, "status cetak belum pasti; periksa printer sebelum mencoba lagi agar tidak tercetak ganda. "+err.Error())
		}
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	_, windowsQueueErr := utils.WindowsQueueName(printer.URI)
	windowsQueue := windowsQueueErr == nil
	if ippJobID > 0 {
		s.logf("INFO", "POST /print/%s -> %s 202 (IPP job %d)", docType, printer.Name, ippJobID)
	} else if windowsQueue {
		s.logf("INFO", "POST /print/%s -> %s 202 (Windows driver)", docType, printer.Name)
	} else {
		s.logf("INFO", "POST /print/%s -> %s 202 (local print queue)", docType, printer.Name)
	}
	response := fiber.Map{
		"message": "Dokumen sudah dikirim ke printer. Periksa hasil cetaknya.",
		"printer": printer.Name,
	}
	if !windowsQueue {
		response["ipp_job_id"] = ippJobID
	}
	return c.Status(fiber.StatusAccepted).JSON(response)
}

func listLocalPrinters(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.UserContext(), 10*time.Second)
	defer cancel()
	var printers []LocalPrinter
	var err error
	switch runtime.GOOS {
	case "windows":
		printers, err = listWindowsPrinters(ctx)
	case "darwin", "linux":
		printers, err = listCUPSPrinters(ctx)
	default:
		return fiber.NewError(fiber.StatusNotImplemented, "deteksi printer belum didukung pada sistem operasi ini")
	}
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "tidak dapat membaca antrean printer pada PC ini: "+err.Error())
	}
	if printers == nil {
		printers = []LocalPrinter{}
	}
	return c.JSON(fiber.Map{"data": printers, "platform": runtime.GOOS})
}

func discoverNetworkPrinters(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.UserContext(), 3*time.Second)
	defer cancel()
	printers, err := discoverIPPPrinters(ctx)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "pencarian Bonjour/mDNS gagal: "+err.Error())
	}
	if printers == nil {
		printers = []discoveredPrinter{}
	}
	return c.JSON(fiber.Map{"data": printers, "platform": runtime.GOOS})
}

func listCUPSPrinters(ctx context.Context) ([]LocalPrinter, error) {
	output, err := exec.CommandContext(ctx, "lpstat", "-p").CombinedOutput()
	if err != nil {
		return nil, commandFailure("lpstat -p", output, err)
	}
	defaultOutput, _ := exec.CommandContext(ctx, "lpstat", "-d").CombinedOutput()
	defaultName := ""
	if index := strings.Index(string(defaultOutput), ":"); index >= 0 {
		defaultName = strings.TrimSpace(string(defaultOutput[index+1:]))
		if defaultName == "none" {
			defaultName = ""
		}
	}
	printers := make([]LocalPrinter, 0)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "printer ") {
			continue
		}
		details := strings.TrimPrefix(line, "printer ")
		separator := strings.Index(details, " is ")
		if separator <= 0 {
			continue
		}
		name := strings.TrimSpace(details[:separator])
		statusText := strings.ToLower(details[separator+4:])
		state := "ready"
		switch {
		case strings.Contains(statusText, "disabled"), strings.Contains(statusText, "paused"):
			state = "disabled"
		case strings.Contains(statusText, "offline"), strings.Contains(statusText, "not responding"):
			state = "offline"
		case strings.Contains(statusText, "printing"):
			state = "printing"
		case !strings.Contains(statusText, "idle") && !strings.Contains(statusText, "enabled"):
			state = "unknown"
		}
		printers = append(printers, LocalPrinter{Name: name, State: state, IsDefault: name == defaultName})
	}
	return printers, nil
}

func listWindowsPrinters(ctx context.Context) ([]LocalPrinter, error) {
	script := `$ErrorActionPreference = 'Stop'; $ports = @{}; Get-CimInstance Win32_TCPIPPrinterPort -ErrorAction SilentlyContinue | ForEach-Object { $ports[$_.Name] = $_.HostAddress }; $items = @(Get-CimInstance Win32_Printer | ForEach-Object { $state = if ($_.WorkOffline -or $_.PrinterStatus -eq 7) { 'offline' } elseif ($_.PrinterStatus -eq 8 -or $_.PrinterStatus -eq 6) { 'disabled' } elseif ($_.PrinterStatus -eq 4) { 'printing' } elseif ($_.PrinterStatus -eq 3 -or $_.PrinterStatus -eq 0) { 'ready' } else { 'unknown' }; [PSCustomObject]@{ name = $_.Name; state = $state; is_default = $_.Default; driver = $_.DriverName; port = $_.PortName; ip = $ports[$_.PortName] } }); ConvertTo-Json -InputObject $items -Compress`
	output, err := hiddenWindowsCommand(ctx, script).CombinedOutput()
	if err != nil {
		return nil, commandFailure("Get-CimInstance Win32_Printer", output, err)
	}
	return parseWindowsPrinters(output)
}

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

type testPrinterRequest struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
	utils.PrinterTestOptions
}

func (s *Server) testLocalPrinter(c *fiber.Ctx) error {
	var request testPrinterRequest
	if err := c.BodyParser(&request); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "pengaturan tes printer tidak valid")
	}
	request.Name = strings.TrimSpace(request.Name)
	request.URI = strings.TrimSpace(request.URI)
	if request.URI == "" && request.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "pilih printer yang akan dites")
	}
	if _, _, _, err := request.PrinterTestOptions.Dimensions(); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if request.Mode != "" && request.Mode != "bw" && request.Mode != "color" {
		return fiber.NewError(fiber.StatusBadRequest, "mode cetak harus hitam putih atau warna")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Second)
	defer cancel()
	if request.URI != "" {
		uri, err := utils.NormalizePrinterURI(request.URI)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		if utils.IsTCPPrinter(uri) {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "TCP 9100 mengirim PDF mentah dan dapat mencetak karakter acak. Pilih antrean printer PC ini melalui IPP (127.0.0.1:631).")
		}
		if request.Name == "" {
			request.Name = uri
		}
		pdf, err := utils.GeneratePrinterTestPDF(request.Name, request.PrinterTestOptions)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "gagal membuat halaman tes: "+err.Error())
		}
		ippJobID, err := submitPrinterPDF(ctx, uri, pdf, "Tes printer", "rsisa", 1, "")
		if err != nil {
			s.logf("ERROR", "tes printer %s gagal: %v", request.Name, err)
			var ippErr *utils.IPPSubmissionError
			if errors.As(err, &ippErr) {
				return fiber.NewError(fiber.StatusBadGateway, "status cetak belum pasti; periksa printer sebelum mencoba lagi agar tidak tercetak ganda. "+err.Error())
			}
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		s.logf("INFO", "halaman uji dikirim ke %s (job %d)", request.Name, ippJobID)
		response := fiber.Map{
			"message": "Halaman uji kualitas dikirim ke antrean cetak. Periksa hasil cetaknya.",
			"printer": request.Name,
			"uri":     uri,
			"state":   "sent",
		}
		if ippJobID > 0 {
			response["ipp_job_id"] = ippJobID
		}
		return c.Status(fiber.StatusAccepted).JSON(response)
	}
	printers, err := listPrintersForPlatform(ctx)
	if err != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "tidak dapat membaca antrean printer pada PC ini: "+err.Error())
	}
	var selected *LocalPrinter
	for index := range printers {
		if printers[index].Name == request.Name {
			selected = &printers[index]
			break
		}
	}
	if selected == nil {
		return fiber.NewError(fiber.StatusNotFound, "printer tidak ditemukan pada PC ini; muat ulang daftar printer")
	}
	if selected.State == "offline" || selected.State == "disabled" {
		return fiber.NewError(fiber.StatusConflict, "printer sedang offline atau dinonaktifkan pada PC ini")
	}
	if err := sendQualityTestPage(ctx, selected.Name, request.PrinterTestOptions); err != nil {
		s.logf("ERROR", "tes printer %s gagal: %v", selected.Name, err)
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	s.logf("INFO", "halaman uji dikirim ke antrean %s", selected.Name)
	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"message": "Halaman uji kualitas dikirim ke antrean printer. Periksa hasil cetaknya.",
		"printer": selected.Name,
		"state":   selected.State,
	})
}

func listPrintersForPlatform(ctx context.Context) ([]LocalPrinter, error) {
	switch runtime.GOOS {
	case "windows":
		return listWindowsPrinters(ctx)
	case "darwin", "linux":
		return listCUPSPrinters(ctx)
	default:
		return nil, fmt.Errorf("OS %q belum didukung", runtime.GOOS)
	}
}

// ListPrinters membaca antrean printer yang dikenal OS pada PC ini.
func ListPrinters(ctx context.Context) ([]LocalPrinter, error) {
	return listPrintersForPlatform(ctx)
}

func sendQualityTestPage(ctx context.Context, printerName string, options utils.PrinterTestOptions) error {
	switch runtime.GOOS {
	case "windows":
		return sendWindowsQualityTest(ctx, printerName, options)
	case "darwin", "linux":
		pdf, err := utils.GeneratePrinterTestPDF(printerName, options)
		if err != nil {
			return err
		}
		return printPDFToCUPS(ctx, printerName, pdf, "RSISA Printer Test", 1, "")
	default:
		return fmt.Errorf("OS %q belum didukung", runtime.GOOS)
	}
}

func submitPrinterPDF(ctx context.Context, uri string, pdf []byte, jobName, userName string, copies int, media string) (int, error) {
	if queueName, err := utils.WindowsQueueName(uri); err == nil {
		if runtime.GOOS != "windows" {
			return 0, fmt.Errorf("antrean Windows hanya dapat dicetak dari Aquila yang berjalan di Windows")
		}
		return 0, printPDFToWindowsQueue(ctx, queueName, pdf, jobName, copies, media)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if queue := localCUPSQueue(uri); queue != "" {
			return 0, printPDFToCUPS(ctx, queue, pdf, jobName, copies, media)
		}
	}
	return utils.PrintIPPDocument(ctx, uri, pdf, jobName, userName, copies, media)
}

func localCUPSQueue(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "ipp" || parsed.Port() != "631" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1") {
		return ""
	}
	queue, ok := strings.CutPrefix(parsed.Path, "/printers/")
	if !ok || queue == "" || strings.ContainsAny(queue, "/\x00") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return queue
}

func printPDFToCUPS(ctx context.Context, printerName string, pdf []byte, jobName string, copies int, media string) error {
	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		return errors.New("dokumen bukan file PDF yang valid")
	}
	if copies < 1 {
		copies = 1
	}
	if copies > 20 {
		return errors.New("jumlah salinan maksimal 20")
	}
	if strings.ContainsAny(media, " \t\r\n") {
		return errors.New("ukuran kertas tidak valid")
	}
	file, err := os.CreateTemp("", "rsisa-print-*.pdf")
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
	args := []string{"-d", printerName, "-t", jobName, "-n", strconv.Itoa(copies), "-o", "job-sheets=none"}
	if media != "" {
		args = append(args, "-o", "media="+media)
	}
	output, err := exec.CommandContext(ctx, "lp", append(args, path)...).CombinedOutput()
	if err != nil {
		return &utils.IPPSubmissionError{Err: commandFailure("cetak melalui antrean CUPS", output, err)}
	}
	return nil
}

func commandFailure(command string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message != "" {
		return fmt.Errorf("%s gagal: %s: %w", command, message, err)
	}
	return fmt.Errorf("%s gagal: %w", command, err)
}

func findRoute(cfg Config, module, documentType string) (RouteConfig, bool) {
	for _, route := range cfg.Routes {
		if strings.EqualFold(route.ModuleCode, module) && strings.EqualFold(route.DocumentType, documentType) {
			return route, true
		}
	}
	return RouteConfig{}, false
}

func findPrinter(cfg Config, name string) (PrinterConfig, bool) {
	for _, printer := range cfg.Printers {
		if strings.EqualFold(printer.Name, name) {
			return printer, true
		}
	}
	return PrinterConfig{}, false
}

func routeKey(module, documentType string) string {
	return strings.ToLower(module + "/" + documentType)
}

func validCode(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

func (s *Server) corsMiddleware(c *fiber.Ctx) error {
	s.mu.RLock()
	allowedOrigins := s.cfg.AllowedOrigin
	s.mu.RUnlock()
	origin := strings.TrimRight(c.Get(fiber.HeaderOrigin), "/")
	allowed := origin == ""
	for _, candidate := range allowedOrigins {
		if origin == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return fiber.NewError(fiber.StatusForbidden, "origin tidak diizinkan")
	}
	if origin != "" {
		c.Set("Access-Control-Allow-Origin", origin)
		c.Set("Vary", "Origin")
	}
	c.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	c.Set("Access-Control-Allow-Headers", "Content-Type, X-RSISA-Printer-Name, X-RSISA-Printer-URI, X-RSISA-Copies, X-RSISA-Media")
	if c.Method() == fiber.MethodOptions {
		if strings.EqualFold(c.Get("Access-Control-Request-Private-Network"), "true") {
			c.Set("Access-Control-Allow-Private-Network", "true")
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
	return c.Next()
}
