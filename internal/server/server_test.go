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
