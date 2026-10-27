package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRequestIDHeaderIsAssigned(t *testing.T) {
	server := NewServer("")

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/api/v1/settings", nil)
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	id := response.Header().Get("X-Request-Id")
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Fatalf("X-Request-Id = %q, want 16 hex characters", id)
	}
}

func TestRequestIDHeaderIsPreserved(t *testing.T) {
	server := NewServer("")

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/api/v1/settings", nil)
	request.Header.Set("X-Request-Id", "client-supplied-1")
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-Id"); got != "client-supplied-1" {
		t.Fatalf("X-Request-Id = %q, want the client value", got)
	}
}

func TestRequestIDRejectsOverlongValue(t *testing.T) {
	server := NewServer("")

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/api/v1/settings", nil)
	request.Header.Set("X-Request-Id", string(make([]byte, 200)))
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	got := response.Header().Get("X-Request-Id")
	if len(got) != 16 {
		t.Fatalf("X-Request-Id = %q, want a generated id instead of the oversized one", got)
	}
}

func TestDiagnosticsEndpoint(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("BKNETWORK_LOG_DIR", logDir)
	writeLogFile(t, logDir, []string{"line one", "line two"})

	server := NewServer("")
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:13335/api/v1/diagnostics", nil)
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	var payload struct {
		GeneratedAt string   `json:"generatedAt"`
		Version     string   `json:"version"`
		OS          string   `json:"os"`
		LogDir      string   `json:"logDir"`
		Logs        []string `json:"logs"`
		Network     any      `json:"network"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.GeneratedAt == "" || payload.OS == "" || payload.LogDir != logDir {
		t.Fatalf("payload = %+v, want timestamp, os and the log directory", payload)
	}
	if len(payload.Logs) != 2 || payload.Logs[1] != "line two" {
		t.Fatalf("logs = %v, want the file tail", payload.Logs)
	}
	if payload.Network == nil {
		t.Fatal("payload has no network section")
	}
}

func TestDiagnosticsRejectsPost(t *testing.T) {
	server := NewServer("")
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:13335/api/v1/diagnostics", nil)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
}

// writeLogFile creates today's log file with the given lines.
func writeLogFile(t *testing.T, dir string, lines []string) {
	t.Helper()
	path := filepath.Join(dir, "bknetwork-"+time.Now().Format("20060102")+".log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write log file: %v", err)
	}
}
