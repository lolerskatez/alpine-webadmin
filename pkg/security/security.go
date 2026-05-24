package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ── CSRF ───────────────────────────────────────────

const csrfCookieName = "__Host-CSRF"
const csrfHeaderName = "X-CSRF-Token"

// GenerateCSRFToken returns a new random 32-byte hex token.
func GenerateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CSRFMiddleware validates the double-submit cookie against the header.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(csrfCookieName)
		if err != nil {
			http.Error(w, "CSRF cookie missing", http.StatusForbidden)
			return
		}
		header := r.Header.Get(csrfHeaderName)
		if header == "" || header != cookie.Value {
			http.Error(w, "CSRF token mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SetCSRFCookie writes the CSRF cookie.
func SetCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: false, // must be readable by JS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})
}

// ── Rate Limiting ──────────────────────────────────

// Limiter implements a per-IP token bucket rate limiter.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	cap     float64
	refill  float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter creates a rate limiter with rate rps and burst 2*rps.
func NewLimiter(rps int) *Limiter {
	f := float64(rps)
	return &Limiter{
		buckets: make(map[string]*bucket),
		cap:     f * 2,
		refill:  f,
	}
}

// Allow returns true if the request from ip is permitted.
func (l *Limiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{tokens: l.cap - 1, last: now}
		l.buckets[ip] = b
		return true
	}

	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * l.refill
	if b.tokens > l.cap {
		b.tokens = l.cap
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Sweep removes stale entries older than ttl.
func (l *Limiter) Sweep(ttl time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-ttl)
	for ip, b := range l.buckets {
		if b.last.Before(cutoff) {
			delete(l.buckets, ip)
		}
	}
}

// ClientIP extracts the real client IP, respecting X-Forwarded-For only if local.
func ClientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "" {
		host = r.RemoteAddr
	}
	// Only trust X-Forwarded-For from loopback (reverse proxy on same host)
	if strings.HasPrefix(host, "127.") || host == "::1" {
		fwd := r.Header.Get("X-Forwarded-For")
		if fwd != "" {
			parts := strings.Split(fwd, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	return host
}

// ── Path Sanitization ──────────────────────────────

// SanitizePath returns an error if the path contains traversal or null bytes.
func SanitizePath(p string) error {
	if strings.Contains(p, "..") {
		return fmt.Errorf("path contains traversal sequence")
	}
	if strings.Contains(p, "\x00") {
		return fmt.Errorf("path contains null byte")
	}
	return nil
}
