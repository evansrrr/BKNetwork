//go:build !windows

package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"bknetwork/internal/server"
)

func runDesktopApp() error {
	srv := server.NewServer("")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		_ = srv.Start(ctx)
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
