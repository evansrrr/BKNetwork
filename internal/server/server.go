package server

import (
	"context"
	"crypto/subtle"
	"errors"
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
	"sync"
	"syscall"
	"time"

	"bknetwork/internal/events"
	"bknetwork/internal/handlers"
	webassets "bknetwork/web"
)

type Server struct {
	httpServer *http.Server
	hub        *events.Hub
	// ready is closed once the listener is bound, so callers can do post-start
	// work (spawning helper processes) without delaying startup.
	ready     chan struct{}
	readyOnce sync.Once
	boundAddr string
}

const DefaultAddr = "127.0.0.1:13335"

const sessionCookieName = "bknetwork_session"

// NewServer builds the server. An empty addr falls back to
// BKNETWORK_LISTEN_ADDR (a debug/deployment override that also lets the
// acceptance suite run next to whatever already occupies the default port)
// and then to DefaultAddr. The guard, CSP and WebSocket origins all derive
// from the address, so a non default port needs no further configuration.
func NewServer(addr string) *Server {
	if addr == "" {
		addr = strings.TrimSpace(os.Getenv("BKNETWORK_LISTEN_ADDR"))
	}
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
	mux.HandleFunc("/api/v1/diagnostics", handlers.DiagnosticsHandler())
	mux.HandleFunc("/ws", handlers.WSHandler(hub, loopbackOrigins(listenPort(addr), "http")))
	// Unknown /api paths must not fall through to the static file handler, which
	// used to answer them with index.html and status 200.
	mux.HandleFunc("/api/", handlers.NotFoundHandler())

	// static files: prefer the executable directory, then the current working directory.
	if webDir, ok := resolveWebDir(); ok {
		mux.Handle("/", http.FileServer(http.Dir(webDir)))
	} else {
		mux.Handle("/", http.FileServer(http.FS(webassets.Assets)))
	}

	var handler http.Handler = mux
	if token := strings.TrimSpace(os.Getenv("BKNETWORK_API_TOKEN")); token != "" {
		handler = localAuthHandler(handler, token)
	} else {
		log.Println("warning: BKNETWORK_API_TOKEN is unset; the local API is unauthenticated " +
			"(cross-site browser requests are still rejected, local processes are not)")
	}
	// The guard wraps the auth handler, so it runs for every entry point
	// (including the unauthenticated legacy one) and before any credential is
	// looked at: a DNS-rebinding or cross-site request is rejected as such
	// instead of answering 401 and revealing whether a cookie would help.
	handler = guardHandler(handler, addr)
	handler = securityHeadersHandler(handler, addr)
	// Outermost: every request, including one the guard rejects, carries an id
	// that shows up in the response header and in the audit log.
	handler = requestIDHandler(handler)

	return &Server{
		hub:   hub,
		ready: make(chan struct{}),
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			IdleTimeout:       90 * time.Second,
			MaxHeaderBytes:    64 << 10,
			// No WriteTimeout on purpose: switching stacks and the macOS
			// authorization prompt legitimately take tens of seconds, and a
			// write timeout would cut the response (and the rollback report)
			// short. Handlers bound themselves instead.
		},
	}
}

// requestIDHandler gives every request an id and echoes it in X-Request-Id, so a
// user can quote one token and the matching audit lines can be found in the log.
func requestIDHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if len(id) < 8 || len(id) > 64 {
			id = handlers.NewRequestID()
		}
		if id != "" {
			w.Header().Set("X-Request-Id", id)
			r = r.WithContext(handlers.WithRequestID(r.Context(), id))
		}
		next.ServeHTTP(w, r)
	})
}

func localAuthHandler(next http.Handler, token string) http.Handler {
	return newLocalAuthHandler(next, token, newSessionStore(maxSessions))
}

func newLocalAuthHandler(next http.Handler, token string, sessions *sessionStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The bootstrap token is only accepted from the URL once, to mint a
		// session. The browser then keeps a random session id in an HttpOnly
		// cookie, so the bootstrap token never lives in the address bar, the
		// history, or a readable cookie.
		if r.URL.Path == "/" {
			if candidate := r.URL.Query().Get("token"); candidate != "" && secureTokenEqual(candidate, token) {
				secret, err := sessions.issue()
				if err != nil {
					log.Printf("auth: failed to create session: %v", err)
					http.Error(w, "unauthorized", http.StatusInternalServerError)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     sessionCookieName,
					Value:    secret,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		// Non-browser callers (the desktop shell) use the bootstrap token as a
		// bearer token.
		if bearer := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); bearer != "" &&
			strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") &&
			secureTokenEqual(bearer, token) {
			next.ServeHTTP(w, r)
			return
		}
		if cookie, err := r.Cookie(sessionCookieName); err == nil && sessions.valid(cookie.Value) {
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
	allowedPort := listenPort(addr)
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
	policy := contentSecurityPolicy(listenPort(addr))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", policy)
		header.Set("X-Content-Type-Options", "nosniff")
		// Also keeps the session token out of Referer headers.
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		header.Set("Cache-Control", cacheControlFor(r.URL.Path))
		next.ServeHTTP(w, r)
	})
}

// cacheControlFor keeps the API and the HTML shell out of every cache. The shell
// is what references the current app.js/styles.css, so a cached copy would run
// an old UI after an update; the assets themselves may be revalidated.
func cacheControlFor(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/"), path == "/", strings.HasSuffix(path, ".html"):
		return "no-store"
	default:
		return "no-cache"
	}
}

// loopbackOrigins lists the origins this server may be reached from. Both the
// WebSocket origin check and the CSP are derived from it, so a non default
// listen port works without touching either.
func loopbackOrigins(port, scheme string) []string {
	if strings.TrimSpace(port) == "" {
		port = "13335"
	}
	return []string{
		scheme + "://127.0.0.1:" + port,
		scheme + "://localhost:" + port,
		scheme + "://[::1]:" + port,
	}
}

func contentSecurityPolicy(port string) string {
	connect := []string{"'self'"}
	connect = append(connect, loopbackOrigins(port, "http")...)
	connect = append(connect, loopbackOrigins(port, "ws")...)
	connect = append(connect, ipv6ProbeOrigin)
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

// listenPort is the port the request guard should accept. When the configured
// port is 0 (ephemeral, used by tests) the real port is only known after Start,
// so the port comparison is skipped instead of rejecting every request.
func listenPort(addr string) string {
	port := portOfAddr(addr)
	if port == "0" {
		return ""
	}
	return port
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

// shutdownTimeout bounds an orderly shutdown. Handlers can legitimately run for
// tens of seconds, but a stuck shutdown must not keep the process alive forever.
const shutdownTimeout = 10 * time.Second

func (s *Server) Start(ctx context.Context) error {
	// Bind synchronously so a port conflict is reported to the caller instead of
	// surfacing later as a value on a channel, and so Ready can be signalled
	// before anything else happens.
	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("http listen error: %w", err)
	}
	s.boundAddr = listener.Addr().String()
	log.Printf("Starting HTTP server on %s\n", s.boundAddr)

	s.readyOnce.Do(func() { close(s.ready) })

	lnErr := make(chan error, 1)
	go func() {
		lnErr <- s.httpServer.Serve(listener)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	// Start can be called repeatedly in tests; drop the registration again.
	defer signal.Stop(sig)

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-lnErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http listen error: %w", err)
		}
	case <-sig:
		return s.shutdown()
	}
	return nil
}

// Ready is closed as soon as the TCP listener is bound and accepting. It is
// never closed when Start fails to bind.
func (s *Server) Ready() <-chan struct{} {
	return s.ready
}

// Addr returns the address that is actually bound, which differs from the
// configured one when port 0 was requested.
func (s *Server) Addr() string {
	if s.boundAddr != "" {
		return s.boundAddr
	}
	return s.httpServer.Addr
}

// shutdown stops the listener and waits for in-flight handlers, but never
// longer than shutdownTimeout (the previous context.Background() had no
// deadline and could hang the process during exit).
func (s *Server) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	err := s.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		log.Printf("shutdown timed out after %s; in-flight handlers were abandoned", shutdownTimeout)
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down server...")
	return s.httpServer.Shutdown(ctx)
}
