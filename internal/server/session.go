package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// maxSessions bounds how many exchanges are remembered. The desktop shell opens
// one window, and a reload reuses the existing cookie, so a handful is plenty.
const maxSessions = 16

// sessionStore keeps the session ids handed out in exchange for the bootstrap
// token. Sessions live as long as the process.
type sessionStore struct {
	mu      sync.Mutex
	max     int
	order   []string
	secrets map[string]struct{}
}

func newSessionStore(max int) *sessionStore {
	if max <= 0 {
		max = maxSessions
	}
	return &sessionStore{max: max, secrets: make(map[string]struct{}, max)}
}

// issue creates a new random session id. The oldest session is forgotten once
// the store is full, which only happens after many re-opens.
func (s *sessionStore) issue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.order) >= s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.secrets, oldest)
	}
	s.order = append(s.order, secret)
	s.secrets[secret] = struct{}{}
	return secret, nil
}

// valid reports whether secret is a session id that was issued by this process.
func (s *sessionStore) valid(secret string) bool {
	if secret == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.secrets[secret]
	return ok
}
