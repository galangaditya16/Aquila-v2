package main

import (
	"net"
	"strings"
	"testing"
)

func TestDiagnosePortReportsEmptyPort(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	port := address[strings.LastIndex(address, ":")+1:]
	_ = listener.Close()

	result, err := DiagnosePort("127.0.0.1:" + port)
	if err != nil {
		t.Fatal(err)
	}
	if result.InUse || len(result.Processes) != 0 {
		t.Fatalf("port kosong dilaporkan terpakai: %+v", result)
	}
}

func TestDiagnosePortReportsCurrentProcess(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	result, err := DiagnosePort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if !result.InUse || len(result.Processes) == 0 {
		t.Fatalf("port terpakai tidak terdeteksi: %+v", result)
	}
	for _, process := range result.Processes {
		if strings.Contains(process, "pid") || strings.Contains(process, "PID") {
			t.Fatalf("hasil tidak boleh memuat PID: %q", process)
		}
	}
}
