package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCacheControlFor(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/", "no-store"},
		{"/index.html", "no-store"},
		{"/api/v1/status", "no-store"},
		{"/api/unknown", "no-store"},
		{"/app.js", "no-cache"},
		{"/styles.css", "no-cache"},
		{"/favicon.svg", "no-cache"},
	}
	for _, tc := range tests {
		if got := cacheControlFor(tc.path); got != tc.want {
			t.Errorf("cacheControlFor(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestUnknownAPIPathReturnsJSON404(t *testing.T) {
	server := NewServer("")
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/api/v1/does-not-exist", nil)
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "not found" {
		t.Fatalf("body = %v, want a not found error", body)
	}
}

func TestResponsesAreNotCached(t *testing.T) {
	server := NewServer("")

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/", "no-store"},
		{"/app.js", "no-cache"},
	} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335"+tc.path, nil)
		response := httptest.NewRecorder()
		server.httpServer.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, response.Code)
		}
		if got := response.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("%s Cache-Control = %q, want %q", tc.path, got, tc.want)
		}
	}
}
