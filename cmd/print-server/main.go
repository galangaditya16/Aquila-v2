package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/RSISA-Semarang/rsisa-print-server/internal/printserver"
)

func main() {
	configPath := os.Getenv("RSISA_PRINT_SERVER_CONFIG")
	if configPath == "" {
		configPath = "print-server.json"
		if executable, err := os.Executable(); err == nil {
			configPath = filepath.Join(filepath.Dir(executable), configPath)
		}
	}
	cfg, err := printserver.LoadConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}
	server := printserver.New(cfg, nil)
	go func() {
		if err := server.Listen(cfg.Listen); err != nil {
			log.Printf("print server berhenti: %v", err)
		}
	}()
	log.Printf("RSISA print server aktif di http://%s (config: %s)", cfg.Listen, configPath)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
