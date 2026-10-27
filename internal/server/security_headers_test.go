package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := securityHeadersHandler(next, DefaultAddr)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	expected := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "no-referrer",
		"X-Frame-Options":            "DENY",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for name, want := range expected {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := response.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("Content-Security-Policy header is missing")
	}
}

func TestContentSecurityPolicyAllowsOnlyRequiredOrigins(t *testing.T) {
	policy := contentSecurityPolicy("13335")

	for _, want := range []string{
		"default-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'none'",
		// The campus portal only exposes JSONP, the IPv6 check is a fetch.
		"script-src 'self' " + campusTrafficOrigin,
		"connect-src 'self' http://127.0.0.1:13335 http://localhost:13335 ws://127.0.0.1:13335 ws://localhost:13335 " + ipv6ProbeOrigin,
	} {
		if !strings.Contains(policy, want) {
			t.Errorf("policy is missing %q:\n%s", want, policy)
		}
	}

	// No blanket inline/eval allowances: the UI was changed to avoid inline
	// handlers and inline styles so this stays strict.
	for _, forbidden := range []string{"unsafe-inline", "unsafe-eval", "script-src *", "default-src *"} {
		if strings.Contains(policy, forbidden) {
			t.Errorf("policy must not contain %q:\n%s", forbidden, policy)
		}
	}
}

func TestContentSecurityPolicyFollowsListenPort(t *testing.T) {
	policy := contentSecurityPolicy(portOfAddr("127.0.0.1:61000"))
	if !strings.Contains(policy, "ws://127.0.0.1:61000") {
		t.Fatalf("policy does not use the configured port:\n%s", policy)
	}
	if strings.Contains(policy, ":13335") {
		t.Fatalf("policy still references the default port:\n%s", policy)
	}

	// An empty port falls back to the default rather than producing "host:".
	if got := contentSecurityPolicy(""); !strings.Contains(got, "ws://127.0.0.1:13335") {
		t.Fatalf("empty port did not fall back to the default:\n%s", got)
	}
}

func TestServerSendsSecurityHeaders(t *testing.T) {
	server := NewServer("")
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("the real handler chain does not send a CSP")
	}
}
