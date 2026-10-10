// Package auth holds the standalone daemon's access control: password hashing
// and the login sessions. Managed mode never uses it (Gextto drives the daemon
// with its token over the API), so nothing here runs unless the process is
// started standalone.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// PasswordHashPrefix marks a bcrypt hash in the settings file, the same
// convention Gextto uses so an operator can read both.
const PasswordHashPrefix = "bcrypt:"

// HashPassword returns a bcrypt hash of value, prefixed so it is recognisable.
// An empty value or an already hashed one is returned unchanged, making the
// call idempotent.
func HashPassword(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, PasswordHashPrefix) {
		return value, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return PasswordHashPrefix + string(hash), nil
}

// VerifyPassword reports whether password matches the stored value. Only a
// hashed value is accepted: a plaintext password in the settings file is never
// compared, so it cannot be used by mistake.
func VerifyPassword(stored, password string) bool {
	stored = strings.TrimSpace(stored)
	if !strings.HasPrefix(stored, PasswordHashPrefix) || password == "" {
		return false
	}
	hash := strings.TrimPrefix(stored, PasswordHashPrefix)
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Sessions is an in-memory table of login sessions. It is intentionally not
// persisted: a restart logs everyone out, which is the safe default.
type Sessions struct {
	mu     sync.Mutex
	ttl    time.Duration
	now    func() time.Time
	tokens map[string]time.Time
}

// NewSessions returns a session table whose entries live for ttl.
func NewSessions(ttl time.Duration) *Sessions {
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &Sessions{ttl: ttl, now: time.Now, tokens: map[string]time.Time{}}
}

// maxSessions bounds the in-memory session table, so a client that logs in
// repeatedly cannot grow it without limit. When full, the entries closest to
// expiry (the oldest logins) are dropped first.
const maxSessions = 1024

// Create makes a new session and returns its token.
func (s *Sessions) Create() string {
	token := randomToken()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = map[string]time.Time{}
	}
	now := s.now()
	// Prune the expired sessions now instead of only when they are used again,
	// so an idle table does not keep dead tokens for the whole TTL.
	for tok, expiry := range s.tokens {
		if !now.Before(expiry) {
			delete(s.tokens, tok)
		}
	}
	if len(s.tokens) >= maxSessions {
		s.dropOldestLocked(len(s.tokens) - (maxSessions - 1))
	}
	s.tokens[token] = now.Add(s.ttl)
	return token
}

// dropOldestLocked removes the count sessions closest to expiry. The caller
// holds s.mu.
func (s *Sessions) dropOldestLocked(count int) {
	if count <= 0 {
		return
	}
	type entry struct {
		token  string
		expiry time.Time
	}
	entries := make([]entry, 0, len(s.tokens))
	for tok, expiry := range s.tokens {
		entries = append(entries, entry{tok, expiry})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].expiry.Before(entries[j].expiry) })
	for i := 0; i < count && i < len(entries); i++ {
		delete(s.tokens, entries[i].token)
	}
}

// Valid reports whether token is a live session, pruning it when expired.
func (s *Sessions) Valid(token string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.tokens[token]
	if !ok {
		return false
	}
	if !s.now().Before(expiry) {
		delete(s.tokens, token)
		return false
	}
	return true
}

// Drop removes a session (logout).
func (s *Sessions) Drop(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

// TTL is the session lifetime.
func (s *Sessions) TTL() time.Duration { return s.ttl }

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail in practice; a zero token would be worse
		// than a panic, but the failure must not be silent.
		panic("auth: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(buf)
}
