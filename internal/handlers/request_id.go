package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type requestIDKey struct{}

// NewRequestID returns a short random identifier for one HTTP request. It ends
// up in the X-Request-Id response header and in every audit line the request
// writes, so a user can quote a single token when reporting a failure.
func NewRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(buf[:])
}

// WithRequestID stores id in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request id assigned by the server middleware.
func RequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}
