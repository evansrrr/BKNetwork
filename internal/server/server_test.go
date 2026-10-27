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
		if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
			t.Fatalf("cookies = %#v, want session cookie", cookies)
		}
		if cookies[0].Value == "" {
			t.Fatal("session cookie is empty")
		}
		// The cookie must not carry the bootstrap token itself.
		if cookies[0].Value == token {
			t.Fatal("session cookie contains the bootstrap token")
		}
		if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("cookie flags = %#v, want HttpOnly and SameSite=Strict", cookies[0])
		}

		// The issued session has to authenticate the follow-up navigation.
		followUp := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		followUp.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookies[0].Value})
		followResponse := httptest.NewRecorder()
		handler.ServeHTTP(followResponse, followUp)
		if followResponse.Code != http.StatusNoContent {
			t.Fatalf("follow-up status = %d, want %d", followResponse.Code, http.StatusNoContent)
		}
	})

	t.Run("each exchange issues a distinct session", func(t *testing.T) {
		seen := make(map[string]struct{})
		for i := 0; i < 3; i++ {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/?token="+token, nil))
			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %#v, want one session cookie", cookies)
			}
			if _, ok := seen[cookies[0].Value]; ok {
				t.Fatal("session id was reused")
			}
			seen[cookies[0].Value] = struct{}{}
		}
	})

	t.Run("reject wrong query token", func(t *testing.T) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/?token=wrong", nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
		if cookies := response.Result().Cookies(); len(cookies) != 0 {
			t.Fatalf("cookies = %#v, want none", cookies)
		}
	})

	t.Run("reject empty query token", func(t *testing.T) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/?token=", nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
	})

	t.Run("reject unknown session cookie", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-session"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
		}
	})

	t.Run("accept bootstrap token as bearer", func(t *testing.T) {
		// The desktop shell reads settings over a raw HTTP request before the
		// webview exists, so the bootstrap token has to keep working there.
		request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		request.Header.Set("Authorization", "Bearer "+token)
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

func TestSessionStoreEvictsOldest(t *testing.T) {
	store := newSessionStore(2)
	first, err := store.issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	second, err := store.issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	third, err := store.issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if store.valid(first) {
		t.Error("oldest session should have been evicted")
	}
	if !store.valid(second) || !store.valid(third) {
		t.Error("recent sessions should still be valid")
	}
	if store.valid("") {
		t.Error("empty secret must not be valid")
	}
}
