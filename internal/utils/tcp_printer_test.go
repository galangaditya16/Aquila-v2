package utils

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestNormalizePrinterURI(t *testing.T) {
	queueURI := WindowsQueueURI(`EPSON L5190\USB: β`)
	if got, err := NormalizePrinterURI(queueURI); err != nil || got != queueURI {
		t.Fatalf("local Windows queue URI = %q, %v; want %q", got, err, queueURI)
	}
	if got, err := WindowsQueueName(queueURI); err != nil || got != `EPSON L5190\USB: β` {
		t.Fatalf("local Windows queue name = %q, %v", got, err)
	}
	for _, invalid := range []string{"winspool://queue/", "winspool://queue/not-base64!", "winspool://other/QQ", "winspool://queue/QQ?x=1"} {
		if _, err := NormalizePrinterURI(invalid); err == nil {
			t.Errorf("accepted invalid Windows queue URI %q", invalid)
		}
	}
	for _, tc := range []struct{ input, want string }{
		{"192.168.1.10", "ipp://192.168.1.10:631/ipp/print"},
		{"tcp://192.168.1.10:9001", "tcp://192.168.1.10:9001"},
		{"tcp://192.168.1.10", "tcp://192.168.1.10:9100"},
		{"socket://192.168.1.10:9100/", "tcp://192.168.1.10:9100"},
		{"ipps://192.168.1.10:8631/custom", "ipps://192.168.1.10:8631/custom"},
		{"tcp://[fd00::1]:9001", "tcp://[fd00::1]:9001"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := NormalizePrinterURI(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, input := range []string{"tcp://192.168.1.10:0", "tcp://192.168.1.10:65536", "tcp://192.168.1.10:", "tcp://192.168.1.10:abc", "tcp://192.168.1.10/job", "tcp://192.168.1.10?x=1", "tcp://192.168.1.10?", "tcp://user@192.168.1.10", "http://192.168.1.10", "tcp://example.com:9001", "tcp://8.8.8.8:9001"} {
		t.Run(input, func(t *testing.T) {
			if _, err := NormalizePrinterURI(input); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func tcpTestListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	_ = l.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second))
	return l
}

func TestPrintTCPDocumentSendsExactPDFPerCopy(t *testing.T) {
	l := tcpTestListener(t)
	pdf := []byte("%PDF-1.4\n\x00\xff\n%%EOF\n")
	results := make(chan []byte, 2)
	go func() {
		defer close(results)
		for range 2 {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			body, _ := io.ReadAll(conn)
			_ = conn.Close()
			results <- body
		}
	}()
	if err := PrintTCPDocument(context.Background(), "tcp://"+l.Addr().String(), pdf, 2, ""); err != nil {
		t.Fatal(err)
	}
	count := 0
	for body := range results {
		count++
		if !bytes.Equal(body, pdf) {
			t.Fatalf("copy differs: %q", body)
		}
	}
	if count != 2 {
		t.Fatalf("received %d copies, want 2", count)
	}
}

func TestInspectTCPDoesNotClaimPDFSupport(t *testing.T) {
	l := tcpTestListener(t)
	received := make(chan []byte, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			received <- nil
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		body, _ := io.ReadAll(conn)
		received <- body
	}()
	caps, err := InspectNetworkPrinter(context.Background(), "tcp://"+l.Addr().String())
	if err != nil || caps.State != "reachable" || caps.PDFSupported || caps.PDFSupportKnown {
		t.Fatalf("unexpected capabilities: %+v, %v", caps, err)
	}
	if body := <-received; len(body) != 0 {
		t.Fatalf("probe sent unexpected bytes: %q", body)
	}
}

func TestPrintTCPCancelledBeforeSendingIsFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := PrintTCPDocument(ctx, "tcp://127.0.0.1:9001", []byte("%PDF-1.4"), 1, "")
	var uncertain *TCPSubmissionError
	if err == nil || errors.As(err, &uncertain) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected definite cancellation, got %v", err)
	}
}

func TestPrintTCPRejectsUnsupportedSettings(t *testing.T) {
	for _, tc := range []struct {
		pdf    string
		copies int
		media  string
	}{{"not pdf", 1, ""}, {"%PDF-1.4", 0, ""}, {"%PDF-1.4", 21, ""}, {"%PDF-1.4", 1, "A4"}} {
		if err := PrintTCPDocument(context.Background(), "tcp://127.0.0.1:9001", []byte(tc.pdf), tc.copies, tc.media); err == nil {
			t.Fatal("unsupported settings accepted")
		}
	}
}

func TestPrintTCPCancelledAfterPartialSendIsUncertain(t *testing.T) {
	l := tcpTestListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			cancel()
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _ = io.ReadFull(conn, make([]byte, 1024))
		cancel()
	}()
	pdf := append([]byte("%PDF-1.4\n"), make([]byte, 20<<20)...)
	err := PrintTCPDocument(ctx, "tcp://"+l.Addr().String(), pdf, 1, "")
	var uncertain *TCPSubmissionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("expected uncertain delivery after partial send, got %v", err)
	}
}
