package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"bknetwork/internal/handlers"
	"bknetwork/internal/server"
	appsettings "bknetwork/internal/settings"
)

func main() {
	cfg, err := appsettings.Load()
	if err != nil {
		log.Printf("failed to load settings: %v", err)
	}

	// Keep the existing scheduled task in sync, but point it at the Tauri
	// executable supplied by the parent process instead of this sidecar.
	if err := appsettings.ApplyStartupShortcut(cfg.AutoStart); err != nil {
		log.Printf("failed to sync autostart setting: %v", err)
	}

	if cfg.WarpAutoStart {
		go func() {
			if err := handlers.StartWarp(); err != nil {
				log.Printf("warp auto start failed: %v", err)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := server.NewServer("").Start(ctx); err != nil {
		log.Fatal(err)
	}
}
