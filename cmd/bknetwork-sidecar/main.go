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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := server.NewServer("")
	serverDone := make(chan error, 1)
	go func() { serverDone <- srv.Start(ctx) }()

	// Wait for the listener before doing anything else: the desktop shell polls
	// for this port, and the work below spawns helper processes (a hidden
	// scheduled task update, warp-cli) that used to run first and delay the
	// window by seconds.
	select {
	case <-srv.Ready():
		go syncAutostart(cfg)
		go startWarpIfEnabled(cfg)
	case err := <-serverDone:
		log.Fatal(err)
	}

	if err := <-serverDone; err != nil {
		log.Fatal(err)
	}
}

// syncAutostart keeps the scheduled task in sync, pointing it at the Tauri
// executable supplied by the parent process rather than this sidecar.
func syncAutostart(cfg appsettings.Settings) {
	if err := appsettings.ApplyStartupShortcut(cfg.AutoStart); err != nil {
		log.Printf("failed to sync autostart setting: %v", err)
	}
}

func startWarpIfEnabled(cfg appsettings.Settings) {
	if !cfg.WarpAutoStart {
		return
	}
	if err := handlers.StartWarp(); err != nil {
		log.Printf("warp auto start failed: %v", err)
	}
}
