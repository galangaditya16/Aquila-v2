package utils

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	ippOperationGetPrinterAttributes uint16 = 0x000b
	ippOperationPrintJob             uint16 = 0x0002
	ippStatusOK                      uint16 = 0x0000
	ippStatusOKIgnored               uint16 = 0x0001
	ippStatusOKConflicting           uint16 = 0x0002
	ippStatusVersionNotSupported     uint16 = 0x0503
	ippTagOperationAttributes        byte   = 0x01
	ippTagJobAttributes              byte   = 0x02
	ippTagEnd                        byte   = 0x03
	ippTagCharset                    byte   = 0x47
	ippTagNaturalLanguage            byte   = 0x48
	ippTagMimeMediaType              byte   = 0x49
	ippTagURI                        byte   = 0x45
	ippTagNameWithoutLanguage        byte   = 0x42
	ippTagKeyword                    byte   = 0x44
	ippTagInteger                    byte   = 0x21
	ippTagEnum                       byte   = 0x23
)

var ippRequestID atomic.Uint32

type IPPPrinterCapabilities struct {
	PrinterName     string   `json:"printer_name"`
	MakeAndModel    string   `json:"make_and_model"`
	State           string   `json:"state"`
	PDFSupportKnown bool     `json:"pdf_support_known"`
	PDFSupported    bool     `json:"pdf_supported"`
	DocumentFormats []string `json:"document_formats"`
	MediaSupported  []string `json:"media_supported"`
}

type IPPSubmissionError struct{ Err error }

func (e *IPPSubmissionError) Error() string { return e.Err.Error() }
func (e *IPPSubmissionError) Unwrap() error { return e.Err }

// InspectIPPPrinter reads the printer's advertised capabilities using IPP.
func InspectIPPPrinter(ctx context.Context, printerURI string) (IPPPrinterCapabilities, error) {
	var result IPPPrinterCapabilities
	parsed, endpoint, err := parseIPPURI(printerURI)
	if err != nil {
		return result, err
	}
	buildRequest := func(requestID uint32, major, minor byte) []byte {
		request := ippRequestHeaderVersion(ippOperationGetPrinterAttributes, requestID, major, minor)
		request = append(request, ippTagOperationAttributes)
		request = appendIPPAttribute(request, ippTagCharset, "attributes-charset", "utf-8")
		request = appendIPPAttribute(request, ippTagNaturalLanguage, "attributes-natural-language", "en")
		request = appendIPPAttribute(request, ippTagURI, "printer-uri", parsed.String())
		request = appendIPPAttributeValues(request, ippTagKeyword, "requested-attributes", "printer-name", "printer-make-and-model", "printer-state", "document-format-supported", "media-supported")
		return append(request, ippTagEnd)
	}
	requestID := ippRequestID.Add(1)
	response, err := sendIPPRequest(ctx, endpoint, parsed, buildRequest(requestID, 2, 0))
	if err != nil {
		return result, err
	}
	status, attrs, err := decodeIPPResponse(response, requestID)
	if err != nil {
		return result, err
	}
	if status == ippStatusVersionNotSupported {
		requestID = ippRequestID.Add(1)
		response, err = sendIPPRequest(ctx, endpoint, parsed, buildRequest(requestID, 1, 1))
		if err != nil {
			return result, err
		}
		status, attrs, err = decodeIPPResponse(response, requestID)
		if err != nil {
			return result, err
		}
	}
	if !ippStatusSuccessful(status) {
		return result, fmt.Errorf("printer menolak pemeriksaan IPP (status 0x%04x)", status)
	}
	result.PDFSupportKnown = true
	result.PrinterName = firstIPPValue(attrs, "printer-name")
	result.MakeAndModel = firstIPPValue(attrs, "printer-make-and-model")
	result.State = ippPrinterState(firstIPPValue(attrs, "printer-state"))
	result.DocumentFormats = attrs["document-format-supported"]
	result.MediaSupported = attrs["media-supported"]
	for _, format := range result.DocumentFormats {
		if strings.EqualFold(format, "application/pdf") {
			result.PDFSupported = true
			break
		}
	}
	return result, nil
}

// PrintIPPDocument sends an already-rendered PDF to a printer that advertises PDF support.
func PrintIPPDocument(ctx context.Context, printerURI string, pdf []byte, jobName, userName string, copies int, media string) (int, error) {
	if len(pdf) < 5 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return 0, errors.New("dokumen bukan file PDF yang valid")
	}
	if copies < 1 {
		copies = 1
	}
	if copies > 20 {
		return 0, errors.New("jumlah salinan maksimal 20")
	}
	caps, err := InspectIPPPrinter(ctx, printerURI)
	if err != nil {
		return 0, err
	}
	if !caps.PDFSupported {
		return 0, errors.New("printer tidak mengiklankan dukungan PDF (application/pdf)")
	}
	if media != "" && !containsIPPValue(caps.MediaSupported, media) {
		return 0, fmt.Errorf("ukuran kertas %q tidak didukung printer", media)
	}
	parsed, endpoint, err := parseIPPURI(printerURI)
	if err != nil {
		return 0, err
	}
	if jobName == "" {
		jobName = "RSISA document"
	}
	if userName == "" {
		userName = "rsisa"
	}
	buildRequest := func(requestID uint32, major, minor byte) []byte {
		request := ippRequestHeaderVersion(ippOperationPrintJob, requestID, major, minor)
		request = append(request, ippTagOperationAttributes)
		request = appendIPPAttribute(request, ippTagCharset, "attributes-charset", "utf-8")
		request = appendIPPAttribute(request, ippTagNaturalLanguage, "attributes-natural-language", "en")
		request = appendIPPAttribute(request, ippTagURI, "printer-uri", parsed.String())
		request = appendIPPAttribute(request, ippTagNameWithoutLanguage, "requesting-user-name", userName)
		request = appendIPPAttribute(request, ippTagNameWithoutLanguage, "job-name", jobName)
		request = appendIPPAttribute(request, ippTagMimeMediaType, "document-format", "application/pdf")
		request = append(request, ippTagJobAttributes)
		request = appendIPPIntegerAttribute(request, "copies", copies)
		if media != "" {
			request = appendIPPAttribute(request, ippTagKeyword, "media", media)
		}
		request = append(request, ippTagEnd)
		return append(request, pdf...)
	}
	requestID := ippRequestID.Add(1)
	response, err := sendIPPRequest(ctx, endpoint, parsed, buildRequest(requestID, 2, 0))
	if err != nil {
		return 0, &IPPSubmissionError{Err: err}
	}
	status, attrs, err := decodeIPPResponse(response, requestID)
	if err != nil {
		return 0, &IPPSubmissionError{Err: err}
	}
	if status == ippStatusVersionNotSupported {
		requestID = ippRequestID.Add(1)
		response, err = sendIPPRequest(ctx, endpoint, parsed, buildRequest(requestID, 1, 1))
		if err != nil {
			return 0, &IPPSubmissionError{Err: err}
		}
		status, attrs, err = decodeIPPResponse(response, requestID)
		if err != nil {
			return 0, &IPPSubmissionError{Err: err}
		}
	}
	if !ippStatusSuccessful(status) {
		return 0, fmt.Errorf("printer menolak dokumen (status IPP 0x%04x)", status)
	}
	return firstIPPInteger(attrs, "job-id"), nil
}

func parseIPPURI(raw string) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "ipp" && parsed.Scheme != "ipps") {
		return nil, "", errors.New("alamat printer harus berupa URI ipp:// atau ipps:// yang valid")
	}
	if parsed.Path == "" {
		parsed.Path = "/ipp/print"
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, "", errors.New("alamat printer tidak boleh memakai query atau fragment")
	}
	port := parsed.Port()
	if port == "" {
		port = "631"
	}
	host := net.JoinHostPort(parsed.Hostname(), port)
	scheme := "http"
	if parsed.Scheme == "ipps" {
		scheme = "https"
	}
	return parsed, scheme + "://" + host + parsed.EscapedPath(), nil
}

func sendIPPRequest(ctx context.Context, endpoint string, printerURI *url.URL, payload []byte) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	if allowUnverifiedIPPS(printerURI) {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		// Testing exception for one explicitly configured local IP and port only.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		defer transport.CloseIdleConnections()
		client.Transport = transport
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("buat permintaan IPP: %w", err)
	}
	request.Header.Set("Content-Type", "application/ipp")
	request.Header.Set("Accept", "application/ipp")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("hubungi printer %s: %w", printerURI.Host, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("printer merespons HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("baca respons printer: %w", err)
	}
	if len(body) >= 2<<20 {
		return nil, errors.New("respons IPP printer terlalu besar")
	}
	return body, nil
}

func allowUnverifiedIPPS(printerURI *url.URL) bool {
	if printerURI.Scheme != "ipps" {
		return false
	}
	ip := net.ParseIP(printerURI.Hostname())
	if ip == nil || !(ip.IsPrivate() || ip.IsLoopback()) {
		return false
	}
	port := printerURI.Port()
	if port == "" {
		port = "631"
	}
	return strings.TrimSpace(os.Getenv("RSISA_INSECURE_IPPS_HOST")) == net.JoinHostPort(ip.String(), port)
}

func ippRequestHeaderVersion(operation uint16, requestID uint32, major, minor byte) []byte {
	buffer := make([]byte, 8)
	buffer[0], buffer[1] = major, minor
	binary.BigEndian.PutUint16(buffer[2:4], operation)
	binary.BigEndian.PutUint32(buffer[4:8], requestID)
	return buffer
}

func appendIPPAttribute(buffer []byte, tag byte, name, value string) []byte {
	buffer = append(buffer, tag)
	buffer = appendIPPBytes(buffer, []byte(name))
	return appendIPPBytes(buffer, []byte(value))
}

func appendIPPAttributeValues(buffer []byte, tag byte, name string, values ...string) []byte {
	for index, value := range values {
		attributeName := name
		if index > 0 {
			attributeName = ""
		}
		buffer = appendIPPAttribute(buffer, tag, attributeName, value)
	}
	return buffer
}

func appendIPPIntegerAttribute(buffer []byte, name string, value int) []byte {
	buffer = append(buffer, ippTagInteger)
	nameBytes := []byte(name)
	buffer = append(buffer, byte(len(nameBytes)>>8), byte(len(nameBytes)))
	buffer = append(buffer, nameBytes...)
	buffer = append(buffer, 0, 4)
	var intBytes [4]byte
	binary.BigEndian.PutUint32(intBytes[:], uint32(value))
	return append(buffer, intBytes[:]...)
}

func appendIPPBytes(buffer, value []byte) []byte {
	buffer = append(buffer, byte(len(value)>>8), byte(len(value)))
	return append(buffer, value...)
}

func decodeIPPResponse(response []byte, requestID uint32) (uint16, map[string][]string, error) {
	if len(response) < 9 {
		return 0, nil, errors.New("respons IPP printer tidak lengkap")
	}
	status := binary.BigEndian.Uint16(response[2:4])
	if got := binary.BigEndian.Uint32(response[4:8]); got != requestID {
		return status, nil, errors.New("request ID pada respons IPP tidak cocok")
	}
	attrs := make(map[string][]string)
	var lastName string
	for offset := 8; offset < len(response); {
		tag := response[offset]
		offset++
		if tag == ippTagEnd {
			return status, attrs, nil
		}
		if tag <= 0x0f {
			continue
		}
		name, next, err := readIPPBytes(response, offset)
		if err != nil {
			return 0, nil, err
		}
		offset = next
		value, next, err := readIPPBytes(response, offset)
		if err != nil {
			return 0, nil, err
		}
		offset = next
		if len(name) > 0 {
			lastName = string(name)
		}
		if lastName == "" {
			continue
		}
		attrs[lastName] = append(attrs[lastName], decodeIPPValue(tag, value))
	}
	return status, attrs, nil
}

func readIPPBytes(data []byte, offset int) ([]byte, int, error) {
	if offset+2 > len(data) {
		return nil, 0, errors.New("respons IPP terpotong")
	}
	length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+length > len(data) {
		return nil, 0, errors.New("atribut IPP terpotong")
	}
	return data[offset : offset+length], offset + length, nil
}

func decodeIPPValue(tag byte, value []byte) string {
	if (tag == ippTagInteger || tag == ippTagEnum) && len(value) == 4 {
		return fmt.Sprint(int32(binary.BigEndian.Uint32(value)))
	}
	return string(value)
}

func ippStatusSuccessful(status uint16) bool {
	return status == ippStatusOK || status == ippStatusOKIgnored || status == ippStatusOKConflicting
}

func firstIPPValue(attributes map[string][]string, name string) string {
	if values := attributes[name]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func firstIPPInteger(attributes map[string][]string, name string) int {
	var value int
	_, _ = fmt.Sscan(firstIPPValue(attributes, name), &value)
	return value
}

func containsIPPValue(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func ippPrinterState(state string) string {
	switch state {
	case "3":
		return "idle"
	case "4":
		return "processing"
	case "5":
		return "stopped"
	default:
		return "unknown"
	}
}
