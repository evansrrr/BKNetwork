package handlers

import (
	"bytes"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditCarriesRequestID(t *testing.T) {
	var buffer bytes.Buffer
	original := log.Writer()
	log.SetOutput(&buffer)
	defer log.SetOutput(original)

	request := httptest.NewRequest("POST", "/api/v1/switch", nil)
	request = request.WithContext(WithRequestID(request.Context(), "abc12345"))

	audit(request, "switch", map[string]string{"ifName": "WiFi", "ok": "true"})

	line := buffer.String()
	if !strings.Contains(line, "AUDIT action=switch") {
		t.Fatalf("line = %q, want an audit entry", line)
	}
	if !strings.Contains(line, "requestId=abc12345") {
		t.Fatalf("line = %q, want the request id from the context", line)
	}
}

func TestAuditWithoutRequestID(t *testing.T) {
	var buffer bytes.Buffer
	original := log.Writer()
	log.SetOutput(&buffer)
	defer log.SetOutput(original)

	audit(nil, "dns", nil)

	line := buffer.String()
	if !strings.Contains(line, "AUDIT action=dns") {
		t.Fatalf("line = %q, want the action", line)
	}
	if strings.Contains(line, "requestId=") {
		t.Fatalf("line = %q, want no request id", line)
	}
}

func TestNewRequestIDFormat(t *testing.T) {
	id := NewRequestID()
	if len(id) != 16 {
		t.Fatalf("NewRequestID() = %q, want 16 characters", id)
	}
	if NewRequestID() == id {
		t.Fatal("NewRequestID() returned the same value twice")
	}
}

func TestRequestIDWithoutContextValue(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	if id := RequestID(request); id != "" {
		t.Fatalf("RequestID() = %q, want empty", id)
	}
	if id := RequestID(nil); id != "" {
		t.Fatalf("RequestID(nil) = %q, want empty", id)
	}
}
