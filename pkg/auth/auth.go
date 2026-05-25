package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Role represents a user permission level.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleRead  Role = "read"
)

// Session holds authenticated user state.
type Session struct {
	Username  string
	Role      Role
	Created   time.Time
	Expires   time.Time
	ClientIP  string
	UserAgent string
}

// Store is an in-memory session map with TTL sweeper support.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]Session
	ttl      time.Duration
}

// NewStore creates a session store with the given TTL.
func NewStore(ttlSeconds int) *Store {
	return &Store{
		sessions: make(map[string]Session),
		ttl:      time.Duration(ttlSeconds) * time.Second,
	}
}

// Create generates a new session and returns the SID.
func (s *Store) Create(username string, role Role) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sid := hex.EncodeToString(b)
	now := time.Now()

	s.mu.Lock()
	s.sessions[sid] = Session{
		Username: username,
		Role:     role,
		Created:  now,
		Expires:  now.Add(s.ttl),
	}
	s.mu.Unlock()
	return sid, nil
}

// CreateWithMetadata generates a session with client metadata and anti-fixation protection.
// If a session already exists for this username, the old session is deleted.
func (s *Store) CreateWithMetadata(username string, role Role, clientIP, userAgent string) (string, error) {
	// Anti-session-fixation: remove any existing sessions for this user
	s.mu.Lock()
	for sid, sess := range s.sessions {
		if sess.Username == username {
			delete(s.sessions, sid)
		}
	}
	s.mu.Unlock()

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sid := hex.EncodeToString(b)
	now := time.Now()

	s.mu.Lock()
	s.sessions[sid] = Session{
		Username:  username,
		Role:      role,
		Created:   now,
		Expires:   now.Add(s.ttl),
		ClientIP:  clientIP,
		UserAgent: userAgent,
	}
	s.mu.Unlock()
	return sid, nil
}

// Get returns the session and true if valid.
func (s *Store) Get(sid string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sid]
	if !ok || time.Now().After(sess.Expires) {
		return Session{}, false
	}
	return sess, true
}

// GetWithRenew returns the session and renews its expiration if valid.
func (s *Store) GetWithRenew(sid string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sid]
	if !ok || time.Now().After(sess.Expires) {
		if ok {
			delete(s.sessions, sid)
		}
		return Session{}, false
	}
	// Sliding window: extend expiration on active use
	sess.Expires = time.Now().Add(s.ttl)
	s.sessions[sid] = sess
	return sess, true
}

// Delete removes a session.
func (s *Store) Delete(sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sid)
}

// Sweep removes expired sessions. Returns count removed.
func (s *Store) Sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	removed := 0
	for sid, sess := range s.sessions {
		if now.After(sess.Expires) {
			delete(s.sessions, sid)
			removed++
		}
	}
	return removed
}

// All returns a copy of all active sessions.
func (s *Store) All() map[string]Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Session, len(s.sessions))
	now := time.Now()
	for sid, sess := range s.sessions {
		if now.Before(sess.Expires) {
			out[sid] = sess
		}
	}
	return out
}

// Count returns the number of active sessions.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// CountByUser returns the number of active sessions for a specific username.
func (s *Store) CountByUser(username string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, sess := range s.sessions {
		if sess.Username == username && now.Before(sess.Expires) {
			count++
		}
	}
	return count
}

// CreateWithLimit generates a session but rejects if user already has maxSessions active.
func (s *Store) CreateWithLimit(username string, role Role, maxSessions int) (string, error) {
	if maxSessions > 0 && s.CountByUser(username) >= maxSessions {
		return "", fmt.Errorf("maximum concurrent sessions reached for user %s", username)
	}
	return s.Create(username, role)
}

// RevokeAllForUser deletes all sessions belonging to a username.
func (s *Store) RevokeAllForUser(username string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for sid, sess := range s.sessions {
		if sess.Username == username {
			delete(s.sessions, sid)
			removed++
		}
	}
	return removed
}

// RequireAuth returns middleware that enforces session authentication.
func RequireAuth(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sid := SessionFromRequest(r)
			if _, ok := store.Get(sid); !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole returns middleware that enforces a minimum role level.
func RequireRole(store *Store, required Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sid := SessionFromRequest(r)
			sess, ok := store.Get(sid)
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			if !roleAtLeast(sess.Role, required) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func roleAtLeast(have, want Role) bool {
	// Only admin and read for MVP. Admin can do everything.
	if want == RoleRead {
		return true // any authenticated user can read
	}
	return have == RoleAdmin
}

// ── Cookie Helpers ─────────────────────────────────

// SetSessionCookie writes the __Host-SID cookie.
func SetSessionCookie(w http.ResponseWriter, sid string, ttl int) {
	http.SetCookie(w, &http.Cookie{
		Name:     "__Host-SID",
		Value:    sid,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   ttl,
	})
}

// ClearSessionCookie invalidates the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "__Host-SID",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// SessionFromRequest extracts the SID from the cookie.
func SessionFromRequest(r *http.Request) string {
	c, err := r.Cookie("__Host-SID")
	if err != nil {
		return ""
	}
	return c.Value
}

// ── Passwords ──────────────────────────────────────

// HashPassword returns a bcrypt hash of the plaintext password.
func HashPassword(plain string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(bytes), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
// bcrypt.CompareHashAndPassword runs in constant time to prevent timing attacks.
func CheckPassword(plain, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	return err == nil
}

// ── Failed Login Tracking ──────────────────────────

// FailedLoginTracker tracks per-IP failed login attempts with lockout.
type FailedLoginTracker struct {
	mu       sync.Mutex
	attempts map[string]*failedEntry
	maxAttempts int
	lockoutDuration time.Duration
}

type failedEntry struct {
	count   int
	lastFail time.Time
	lockedUntil time.Time
}

// NewFailedLoginTracker creates a tracker. After maxAttempts within lockoutDuration,
// further attempts from that IP are rejected.
func NewFailedLoginTracker(maxAttempts int, lockoutDuration time.Duration) *FailedLoginTracker {
	return &FailedLoginTracker{
		attempts: make(map[string]*failedEntry),
		maxAttempts: maxAttempts,
		lockoutDuration: lockoutDuration,
	}
}

// RecordFailure increments the failure count for an IP.
func (t *FailedLoginTracker) RecordFailure(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	ent, ok := t.attempts[ip]
	if !ok {
		ent = &failedEntry{}
		t.attempts[ip] = ent
	}
	ent.count++
	ent.lastFail = now
	if ent.count >= t.maxAttempts {
		ent.lockedUntil = now.Add(t.lockoutDuration)
	}
}

// RecordSuccess clears failures for an IP.
func (t *FailedLoginTracker) RecordSuccess(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, ip)
}

// IsLocked returns true if the IP is currently locked out.
func (t *FailedLoginTracker) IsLocked(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	ent, ok := t.attempts[ip]
	if !ok {
		return false
	}
	now := time.Now()
	if now.Before(ent.lockedUntil) {
		return true
	}
	// Lockout expired, reset counter
	if ent.count >= t.maxAttempts && now.After(ent.lockedUntil) {
		ent.count = 0
		ent.lockedUntil = time.Time{}
	}
	return false
}

// Sweep removes stale entries older than ttl.
func (t *FailedLoginTracker) Sweep(ttl time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	for ip, ent := range t.attempts {
		if ent.lastFail.Before(cutoff) {
			delete(t.attempts, ip)
		}
	}
}

// ── Constant-Time Comparison ───────────────────────

// ConstantTimeCompare compares two strings in constant time to prevent
// timing side-channels. Returns 1 if equal, 0 otherwise.
func ConstantTimeCompare(a, b string) int {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b))
}
