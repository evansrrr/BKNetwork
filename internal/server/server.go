package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"bknetwork/internal/events"
	"bknetwork/internal/handlers"
)

type Server struct {
	httpServer *http.Server
	hub        *events.Hub
}

const DefaultAddr = "127.0.0.1:13335"

func NewServer(addr string) *Server {
	if addr == "" {
		addr = DefaultAddr
	}
	mux := http.NewServeMux()
	hub := events.NewHub()
	mux.HandleFunc("/api/v1/switch", handlers.SwitchStackHandler(hub))
	mux.HandleFunc("/api/v1/dns", handlers.DnsHandler(hub))
	mux.HandleFunc("/api/v1/warp", handlers.WarpHandler(hub))
	mux.HandleFunc("/api/v1/warp-status", handlers.WarpStatusHandler())
	mux.HandleFunc("/api/v1/settings", handlers.SettingsHandler(hub))
	mux.HandleFunc("/api/v1/status", handlers.StatusHandler(hub))
	mux.HandleFunc("/api/v1/version/latest", handlers.LatestVersionHandler())
	mux.HandleFunc("/ws", handlers.WSHandler(hub))

	// static files: prefer the executable directory, then the current working directory.
	if webDir, ok := resolveWebDir(); ok {
		mux.Handle("/", http.FileServer(http.Dir(webDir)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
	}

	return &Server{
		hub: hub,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

func resolveWebDir() (string, bool) {
	if exePath, err := os.Executable(); err == nil {
		if dir := filepath.Join(filepath.Dir(exePath), "web"); isDir(dir) {
			return dir, true
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if dir := filepath.Join(cwd, "web"); isDir(dir) {
			return dir, true
		}
	}
	return "", false
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (s *Server) Start(ctx context.Context) error {
	// Start server and listen for termination signals
	lnErr := make(chan error, 1)
	go func() {
		log.Printf("Starting HTTP server on %s\n", s.httpServer.Addr)
		lnErr <- s.httpServer.ListenAndServe()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	case err := <-lnErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http listen error: %w", err)
		}
	case <-sig:
		return s.Shutdown(context.Background())
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down server...")
	return s.httpServer.Shutdown(ctx)
}
