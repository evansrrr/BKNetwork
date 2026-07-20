package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"bknetwork/internal/events"
	"bknetwork/internal/handlers"
	webassets "bknetwork/web"
)

type Server struct {
	httpServer *http.Server
	hub        *events.Hub
}

const DefaultAddr = "127.0.0.1:13335"

const sessionCookieName = "bknetwork_session"

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
		mux.Handle("/", http.FileServer(http.FS(webassets.Assets)))
	}

	var handler http.Handler = mux
	if token := strings.TrimSpace(os.Getenv("BKNETWORK_API_TOKEN")); token != "" {
		handler = localAuthHandler(mux, token)
	}

	return &Server{
		hub: hub,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: handler,
		},
	}
}

func localAuthHandler(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && secureTokenEqual(r.URL.Query().Get("token"), token) {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") &&
			secureTokenEqual(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), token) {
			next.ServeHTTP(w, r)
			return
		}
		if cookie, err := r.Cookie(sessionCookieName); err == nil && secureTokenEqual(cookie.Value, token) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func secureTokenEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
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
