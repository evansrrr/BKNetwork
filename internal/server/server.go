package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
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
	// The guard runs for every entry point, including the unauthenticated
	// legacy one: it blocks DNS-rebinding (via Host) and cross-site browser
	// requests (via Origin and Content-Type) before they reach any handler.
	handler = guardHandler(handler, addr)
	if token := strings.TrimSpace(os.Getenv("BKNETWORK_API_TOKEN")); token != "" {
		handler = localAuthHandler(handler, token)
	} else {
		log.Println("warning: BKNETWORK_API_TOKEN is unset; the local API is unauthenticated " +
			"(cross-site browser requests are still rejected, local processes are not)")
	}
	handler = securityHeadersHandler(handler, addr)

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

// guardHandler rejects requests that cannot come from the local UI: a Host
// that is not a loopback address (DNS rebinding), a foreign Origin, or a
// state-changing request without a JSON content type (which a cross-origin
// HTML form or a "simple" fetch could otherwise send without a preflight).
func guardHandler(next http.Handler, addr string) http.Handler {
	allowedPort := portOfAddr(addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRequestHost(r.Host, allowedPort) {
			log.Printf("guard: rejected request with host %q from %s", r.Host, r.RemoteAddr)
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin, allowedPort) {
			log.Printf("guard: rejected request with origin %q", origin)
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if isStateChangingMethod(r.Method) && !isJSONRequest(r) {
			http.Error(w, "unsupported media type: application/json required", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isJSONRequest(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

// campusTrafficOrigin hosts the JSONP endpoint the UI uses for the campus
// traffic counter. It is the only third-party script origin that is allowed.
const campusTrafficOrigin = "http://202.204.48.66:801"

// ipv6ProbeOrigin is the external echo service used by the "IPv6 check" button.
const ipv6ProbeOrigin = "https://api-ipv6.ip.sb"

// securityHeadersHandler adds browser hardening headers. Without a CSP the
// page injected third-party (campus portal, plain HTTP) scripts with full
// access to the privileged local API.
func securityHeadersHandler(next http.Handler, addr string) http.Handler {
	policy := contentSecurityPolicy(portOfAddr(addr))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", policy)
		header.Set("X-Content-Type-Options", "nosniff")
		// Also keeps the session token out of Referer headers.
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy builds the policy for the UI served on port. The
// WebSocket origins have to be listed explicitly: 'self' does not cover ws://.
func contentSecurityPolicy(port string) string {
	if strings.TrimSpace(port) == "" {
		port = "13335"
	}
	connect := []string{
		"'self'",
		"http://127.0.0.1:" + port,
		"http://localhost:" + port,
		"ws://127.0.0.1:" + port,
		"ws://localhost:" + port,
		ipv6ProbeOrigin,
	}
	directives := []string{
		"default-src 'self'",
		"base-uri 'none'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"form-action 'none'",
		"img-src 'self' data:",
		"font-src 'self'",
		"style-src 'self'",
		"script-src 'self' " + campusTrafficOrigin,
		"connect-src " + strings.Join(connect, " "),
	}
	return strings.Join(directives, "; ") + ";"
}

func portOfAddr(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return ""
}

// splitRequestHost separates the host and port of r.Host without requiring a
// port to be present.
func splitRequestHost(hostport string) (host string, port string) {
	hostport = strings.TrimSpace(hostport)
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		return h, p
	}
	return strings.Trim(hostport, "[]"), ""
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackRequestHost(hostport, allowedPort string) bool {
	host, port := splitRequestHost(hostport)
	if !isLoopbackHost(host) {
		return false
	}
	if allowedPort != "" && port != "" && port != allowedPort {
		return false
	}
	return true
}

func isLoopbackOrigin(origin, allowedPort string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	if !isLoopbackHost(host) {
		return false
	}
	if allowedPort != "" && parsed.Port() != "" && parsed.Port() != allowedPort {
		return false
	}
	return true
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
