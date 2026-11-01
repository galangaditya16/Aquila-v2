package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NormalizePrinterURI preserves existing IP-only IPP configurations while allowing
// explicit raw TCP destinations. A raw port does not imply PDF format support.
func NormalizePrinterURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		ip := net.ParseIP(strings.Trim(raw, "[]"))
		if ip == nil {
			return "", errors.New("masukkan IP printer yang valid")
		}
		raw = "ipp://" + net.JoinHostPort(ip.String(), "631") + "/ipp/print"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("alamat printer tidak valid")
	}
	if u.Scheme == windowsQueueScheme {
		if _, err := WindowsQueueName(raw); err != nil {
			return "", err
		}
		return raw, nil
	}
	if u.Scheme == "socket" {
		u.Scheme = "tcp"
	}
	if u.Scheme != "tcp" && u.Scheme != "ipp" && u.Scheme != "ipps" {
		return "", errors.New("pilih koneksi TCP, IPP, atau IPPS")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !(ip.IsPrivate() || ip.IsLoopback()) {
		return "", errors.New("alamat printer harus memakai IP lokal/private")
	}
	port := u.Port()
	if port == "" {
		if strings.HasSuffix(u.Host, ":") {
			return "", errors.New("port printer wajib diisi")
		}
		port = "631"
		if u.Scheme == "tcp" {
			port = "9100"
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return "", errors.New("port printer harus antara 1 dan 65535")
	}
	u.Host = net.JoinHostPort(ip.String(), strconv.Itoa(portNumber))
	if u.Scheme == "tcp" {
		if u.Path != "" && u.Path != "/" {
			return "", errors.New("alamat TCP hanya berisi IP dan port")
		}
		u.Path, u.RawPath = "", ""
	} else if u.Path == "" {
		u.Path = "/ipp/print"
	}
	return u.String(), nil
}

func IsTCPPrinter(uri string) bool {
	return strings.HasPrefix(uri, "tcp://") || strings.HasPrefix(uri, "socket://")
}

func InspectNetworkPrinter(ctx context.Context, uri string) (IPPPrinterCapabilities, error) {
	uri, err := NormalizePrinterURI(uri)
	if err != nil {
		return IPPPrinterCapabilities{}, err
	}
	if !IsTCPPrinter(uri) {
		return InspectIPPPrinter(ctx, uri)
	}
	u, _ := url.Parse(uri)
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return IPPPrinterCapabilities{}, fmt.Errorf("server aplikasi tidak dapat menghubungi printer %s; periksa IP, port, dan koneksi jaringan dari server: %w", u.Host, err)
	}
	_ = conn.Close()
	// Connecting proves reachability only. Do not claim the printer supports PDF.
	return IPPPrinterCapabilities{State: "reachable", DocumentFormats: []string{}, MediaSupported: []string{}}, nil
}

// TCPSubmissionError means some bytes may have reached the printer. Never retry
// this job automatically, including when an earlier copy has already been sent.
type TCPSubmissionError struct{ Err error }

func (e *TCPSubmissionError) Error() string { return e.Err.Error() }
func (e *TCPSubmissionError) Unwrap() error { return e.Err }

// PrintTCPDocument sends the PDF unchanged, one connection per copy. The printer
// must support direct PDF input on the configured port. TCP has no job receipt
// or physical-print acknowledgement; successful writes mean only "sent".
func PrintTCPDocument(ctx context.Context, uri string, pdf []byte, copies int, media string) error {
	uri, err := NormalizePrinterURI(uri)
	if err != nil {
		return err
	}
	if !IsTCPPrinter(uri) || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return errors.New("tujuan TCP dan dokumen PDF yang valid diperlukan")
	}
	if copies < 1 || copies > 20 {
		return errors.New("jumlah salinan harus antara 1 dan 20")
	}
	if media != "" {
		return errors.New("untuk TCP, atur ukuran kertas pada dokumen dan printer; pilih ukuran Otomatis")
	}
	u, _ := url.Parse(uri)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for copyNumber := 1; copyNumber <= copies; copyNumber++ {
		connected, err := sendTCPCopy(ctx, u.Host, pdf)
		if err != nil {
			failure := fmt.Errorf("pengiriman ke %s pada salinan %d dari %d gagal: %w", u.Host, copyNumber, copies, err)
			if connected || copyNumber > 1 {
				return &TCPSubmissionError{Err: failure}
			}
			return failure
		}
	}
	return nil
}

func sendTCPCopy(ctx context.Context, address string, pdf []byte) (bool, error) {
	conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return false, fmt.Errorf("server aplikasi tidak dapat terhubung ke printer: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return true, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_, err = io.Copy(conn, bytes.NewReader(pdf))
	if err != nil {
		return true, err
	}
	return true, conn.Close()
}
