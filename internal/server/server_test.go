package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalAuthHandler(t *testing.T) {
	const token = "test-token"
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := localAuthHandler(next, token)

	t.Run("reject missing token", func(t *testing.T) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
	})

	t.Run("accept bearer token", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
	})

	t.Run("exchange query token for cookie", func(t *testing.T) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/?token="+token, nil))
		if response.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].Value != token {
			t.Fatalf("cookies = %#v, want session cookie", cookies)
		}
		if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("cookie flags = %#v, want HttpOnly and SameSite=Strict", cookies[0])
		}
	})

	t.Run("accept session cookie", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
		}
	})
}

func TestGuardHandlerHostAndOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := guardHandler(next, DefaultAddr)

	tests := []struct {
		name   string
		host   string
		origin string
		want   int
	}{
		{"loopback with port", "127.0.0.1:13335", "", http.StatusNoContent},
		{"localhost with port", "localhost:13335", "", http.StatusNoContent},
		{"ipv6 loopback", "[::1]:13335", "", http.StatusNoContent},
		{"same origin", "127.0.0.1:13335", "http://127.0.0.1:13335", http.StatusNoContent},
		{"localhost origin", "localhost:13335", "http://localhost:13335", http.StatusNoContent},
		{"foreign host", "attacker.example:13335", "", http.StatusForbidden},
		{"foreign host without port", "attacker.example", "", http.StatusForbidden},
		{"private address rebinding target", "192.168.1.10:13335", "", http.StatusForbidden},
		{"wrong port", "127.0.0.1:8080", "", http.StatusForbidden},
		{"foreign origin", "127.0.0.1:13335", "https://attacker.example", http.StatusForbidden},
		{"foreign origin on same port", "127.0.0.1:13335", "http://127.0.0.1:8080", http.StatusForbidden},
		{"null origin", "127.0.0.1:13335", "null", http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Host = tc.host
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
		})
	}
}

func TestGuardHandlerRequiresJSONBody(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := guardHandler(next, DefaultAddr)

	tests := []struct {
		name        string
		method      string
		contentType string
		want        int
	}{
		{"post json", http.MethodPost, "application/json", http.StatusNoContent},
		{"post json with charset", http.MethodPost, "application/json; charset=utf-8", http.StatusNoContent},
		{"post text plain", http.MethodPost, "text/plain", http.StatusUnsupportedMediaType},
		{"post form", http.MethodPost, "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"post without content type", http.MethodPost, "", http.StatusUnsupportedMediaType},
		{"get without content type", http.MethodGet, "", http.StatusNoContent},
		{"head without content type", http.MethodHead, "", http.StatusNoContent},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/api/v1/switch", nil)
			request.Host = "127.0.0.1:13335"
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
		})
	}
}

// TestServerRejectsForeignHost makes sure the guard is wired into the real
// handler chain and not only available as a helper.
func TestServerRejectsForeignHost(t *testing.T) {
	server := NewServer("")

	evil := httptest.NewRequest(http.MethodPost, "http://attacker.example/api/v1/switch", nil)
	evil.Host = "attacker.example"
	evil.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, evil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign host status = %d, want %d", response.Code, http.StatusForbidden)
	}

	// A same-site request must still reach the mux (the static handler serves
	// the embedded UI, so a 200 here means the guard did not reject it).
	ok := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/", nil)
	response = httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, ok)
	if response.Code != http.StatusOK {
		t.Fatalf("loopback request status = %d, want %d", response.Code, http.StatusOK)
	}
}
