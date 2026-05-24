package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ── CSRF ───────────────────────────────────────────

func TestCSRFMiddlewareGET(t *testing.T) {
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET status = %d, want 200", rr.Code)
	}
}

func TestCSRFMiddlewareMissingCookie(t *testing.T) {
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("POST", "/api/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestCSRFMiddlewareMismatch(t *testing.T) {
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token-a"})
	req.Header.Set(csrfHeaderName, "token-b")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestCSRFMiddlewareMatch(t *testing.T) {
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "matching-token"})
	req.Header.Set(csrfHeaderName, "matching-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestGenerateCSRFToken(t *testing.T) {
	tok1, err := GenerateCSRFToken()
	if err != nil {
		t.Fatalf("GenerateCSRFToken failed: %v", err)
	}
	if len(tok1) != 64 { // 32 bytes hex = 64 chars
		t.Fatalf("token length = %d, want 64", len(tok1))
	}
	tok2, _ := GenerateCSRFToken()
	if tok1 == tok2 {
		t.Fatal("two tokens should differ")
	}
}

func TestSetCSRFCookie(t *testing.T) {
	rr := httptest.NewRecorder()
	SetCSRFCookie(rr, "testtoken")

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "__Host-CSRF" {
		t.Errorf("name = %q, want __Host-CSRF", c.Name)
	}
	if c.HttpOnly {
		t.Error("CSRF cookie must NOT be HttpOnly (JS needs to read it)")
	}
	if !c.Secure {
		t.Error("CSRF cookie should be Secure")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Error("CSRF cookie should be SameSite=Strict")
	}
}

// ── Rate Limiting ──────────────────────────────────

func TestRateLimiterAllow(t *testing.T) {
	l := NewLimiter(10) // 10 rps, burst 20

	// Should allow burst
	for i := 0; i < 20; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed within burst", i+1)
		}
	}

	// 21st should be denied
	if l.Allow("1.2.3.4") {
		t.Fatal("21st request should be rate limited")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	l := NewLimiter(10)

	for i := 0; i < 20; i++ {
		l.Allow("1.2.3.4")
	}

	// Different IP should still be allowed
	if !l.Allow("5.6.7.8") {
		t.Fatal("different IP should not be rate limited")
	}
}

func TestRateLimiterSweep(t *testing.T) {
	l := NewLimiter(1)
	l.Allow("1.2.3.4")

	l.Sweep(1 * time.Nanosecond)

	// After sweep, old entry should be gone, so request is allowed again
	if !l.Allow("1.2.3.4") {
		t.Fatal("after sweep, IP should be allowed again")
	}
}

// ── ClientIP ───────────────────────────────────────

func TestClientIPDirect(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	if ip := ClientIP(req); ip != "192.168.1.50" {
		t.Errorf("ClientIP = %q, want 192.168.1.50", ip)
	}
}

func TestClientIPXForwardedForLoopback(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
	if ip := ClientIP(req); ip != "10.0.0.1" {
		t.Errorf("ClientIP = %q, want 10.0.0.1", ip)
	}
}

func TestClientIPXForwardedForUntrusted(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	// From non-loopback, X-Forwarded-For should NOT be trusted
	if ip := ClientIP(req); ip != "192.168.1.50" {
		t.Errorf("ClientIP = %q, want 192.168.1.50 (untrusted XFF)", ip)
	}
}

// ── Path Sanitization ──────────────────────────────

func TestSanitizePathValid(t *testing.T) {
	if err := SanitizePath("/api/services/nginx"); err != nil {
		t.Errorf("valid path rejected: %v", err)
	}
}

func TestSanitizePathTraversal(t *testing.T) {
	if err := SanitizePath("../etc/passwd"); err == nil {
		t.Error("traversal path should be rejected")
	}
}

func TestSanitizePathNullByte(t *testing.T) {
	if err := SanitizePath("/api/test\x00foo"); err == nil {
		t.Error("null byte path should be rejected")
	}
}

// ── Benchmarks ─────────────────────────────────────

func BenchmarkCSRFMiddleware(b *testing.B) {
	handler := CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token"})
	req.Header.Set(csrfHeaderName, "token")
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkRateLimiterAllow(b *testing.B) {
	l := NewLimiter(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Allow("1.2.3.4")
	}
}
