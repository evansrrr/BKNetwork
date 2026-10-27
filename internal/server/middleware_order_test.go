package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGuardRunsBeforeAuth pins the middleware order: a request with a
// non-loopback Host must be rejected as a rebinding attempt (403) even without
// a session, instead of answering 401 and telling the caller that a cookie
// would get them further.
func TestGuardRunsBeforeAuth(t *testing.T) {
	t.Setenv("BKNETWORK_API_TOKEN", "order-token")
	server := NewServer("")

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/", nil)
	request.Host = "attacker.example"
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 from the host guard", response.Code)
	}
}

func TestGuardRunsBeforeAuthWithValidSession(t *testing.T) {
	t.Setenv("BKNETWORK_API_TOKEN", "order-token")
	server := NewServer("")

	session := httptest.NewRecorder()
	exchange := httptest.NewRequest(http.MethodGet, "/?token=order-token", nil)
	exchange.Host = "127.0.0.1:13335"
	server.httpServer.Handler.ServeHTTP(session, exchange)
	cookies := session.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("token exchange did not set a session cookie")
	}

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/api/v1/settings", nil)
	request.Host = "attacker.example"
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 even with a valid session", response.Code)
	}
}
