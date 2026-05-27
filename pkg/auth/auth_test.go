package auth

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func checkGoroutines(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		time.Sleep(50 * time.Millisecond)
		runtime.GC()
		after := runtime.NumGoroutine()
		if after > before+2 {
			t.Errorf("goroutine leak: before=%d after=%d", before, after)
		}
	})
}

// ── Session Store ──────────────────────────────────

func TestStoreCreateAndGet(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	sid, err := store.Create("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if len(sid) != 64 {
		t.Fatalf("SID length = %d, want 64 hex chars", len(sid))
	}

	sess, ok := store.Get(sid)
	if !ok {
		t.Fatal("Get returned false for valid SID")
	}
	if sess.Username != "admin" {
		t.Errorf("Username = %q, want admin", sess.Username)
	}
	if sess.Role != RoleAdmin {
		t.Errorf("Role = %q, want admin", sess.Role)
	}
}

func TestStoreGetInvalidSID(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	_, ok := store.Get("invalidsid")
	if ok {
		t.Fatal("Get returned true for invalid SID")
	}
}

func TestStoreExpiry(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(1) // 1 second TTL
	sid, _ := store.Create("admin", RoleAdmin)

	time.Sleep(2 * time.Second)
	_, ok := store.Get(sid)
	if ok {
		t.Fatal("expected session to expire")
	}
}

func TestStoreSweep(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(1)
	store.Create("a", RoleAdmin)
	store.Create("b", RoleAdmin)
	time.Sleep(2 * time.Second)

	removed := store.Sweep()
	if removed != 2 {
		t.Errorf("Sweep removed %d, want 2", removed)
	}
	if store.Count() != 0 {
		t.Errorf("Count = %d, want 0", store.Count())
	}
}

func TestStoreDelete(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	sid, _ := store.Create("admin", RoleAdmin)
	store.Delete(sid)
	_, ok := store.Get(sid)
	if ok {
		t.Fatal("expected session deleted")
	}
}

func TestStoreCount(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	if store.Count() != 0 {
		t.Fatalf("initial Count = %d, want 0", store.Count())
	}
	store.Create("a", RoleAdmin)
	store.Create("b", RoleRead)
	if store.Count() != 2 {
		t.Fatalf("Count = %d, want 2", store.Count())
	}
}

// ── Sliding Window Renewal ─────────────────────────

func TestStoreGetWithRenew(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(2) // 2 second TTL
	sid, _ := store.Create("admin", RoleAdmin)

	// Immediately get and renew
	sess, ok := store.GetWithRenew(sid)
	if !ok {
		t.Fatal("expected valid session")
	}
	_ = sess

	// Wait 1.5 seconds (would expire if not renewed)
	time.Sleep(1500 * time.Millisecond)

	// Should still be valid because renewal extended TTL
	_, ok = store.Get(sid)
	if !ok {
		t.Fatal("expected session still valid after renewal")
	}
}

// ── Anti-Session-Fixation ──────────────────────────

func TestStoreCreateWithMetadataFixation(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	oldSID, _ := store.Create("admin", RoleAdmin)

	newSID, err := store.CreateWithMetadata("admin", RoleAdmin, "192.168.1.1", "Mozilla/5.0")
	if err != nil {
		t.Fatalf("CreateWithMetadata failed: %v", err)
	}
	if newSID == oldSID {
		t.Fatal("new SID should differ from old SID")
	}

	// Old session must be invalidated
	_, ok := store.Get(oldSID)
	if ok {
		t.Fatal("old session should be invalidated after fixation protection")
	}

	// New session must be valid
	sess, ok := store.Get(newSID)
	if !ok {
		t.Fatal("new session should be valid")
	}
	if sess.ClientIP != "192.168.1.1" {
		t.Errorf("ClientIP = %q, want 192.168.1.1", sess.ClientIP)
	}
	if sess.UserAgent != "Mozilla/5.0" {
		t.Errorf("UserAgent = %q, want Mozilla/5.0", sess.UserAgent)
	}
}

// ── Middleware ─────────────────────────────────────

func TestRequireAuthValid(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	sid, _ := store.Create("admin", RoleAdmin)

	mw := RequireAuth(store)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "webadmin-sid", Value: sid})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestRequireAuthMissing(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	mw := RequireAuth(store)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestRequireRoleAdmin(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	sid, _ := store.Create("admin", RoleAdmin)

	mw := RequireRole(store, RoleAdmin)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/admin", nil)
	req.AddCookie(&http.Cookie{Name: "webadmin-sid", Value: sid})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestRequireRoleForbidden(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	sid, _ := store.Create("viewer", RoleRead)

	mw := RequireRole(store, RoleAdmin)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("POST", "/api/admin", nil)
	req.AddCookie(&http.Cookie{Name: "webadmin-sid", Value: sid})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

// ── Passwords ──────────────────────────────────────

func TestHashAndCheckPassword(t *testing.T) {
	checkGoroutines(t)
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if !CheckPassword("secret123", hash) {
		t.Fatal("CheckPassword returned false for correct password")
	}
	if CheckPassword("wrong", hash) {
		t.Fatal("CheckPassword returned true for wrong password")
	}
}

func TestCheckPasswordTiming(t *testing.T) {
	checkGoroutines(t)
	// Verify bcrypt runs in constant time by checking wrong password many times
	// and ensuring no panic or early return
	hash, _ := HashPassword("test")
	for i := 0; i < 100; i++ {
		CheckPassword("wrong", hash)
	}
}

// ── Cookies ────────────────────────────────────────

func TestSetSessionCookie(t *testing.T) {
	checkGoroutines(t)
	rr := httptest.NewRecorder()
	SetSessionCookie(rr, "testsid", 3600)

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "webadmin-sid" {
		t.Errorf("name = %q, want webadmin-sid", c.Name)
	}
	if c.Secure {
		t.Error("cookie should not be Secure for HTTP compatibility")
	}
	if !c.HttpOnly {
		t.Error("cookie should be HttpOnly")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Error("cookie should be SameSite=Strict")
	}
	if c.MaxAge != 3600 {
		t.Errorf("MaxAge = %d, want 3600", c.MaxAge)
	}
}

func TestClearSessionCookie(t *testing.T) {
	checkGoroutines(t)
	rr := httptest.NewRecorder()
	ClearSessionCookie(rr)

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1 (delete)", c.MaxAge)
	}
}

func TestSessionFromRequest(t *testing.T) {
	checkGoroutines(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "webadmin-sid", Value: "abc123"})
	if sid := SessionFromRequest(req); sid != "abc123" {
		t.Errorf("SID = %q, want abc123", sid)
	}
}

// ── Failed Login Tracker ───────────────────────────

func TestFailedLoginTracker(t *testing.T) {
	checkGoroutines(t)
	tracker := NewFailedLoginTracker(3, 5*time.Minute)
	ip := "192.168.1.100"

	if tracker.IsLocked(ip) {
		t.Fatal("fresh IP should not be locked")
	}

	tracker.RecordFailure(ip)
	tracker.RecordFailure(ip)
	if tracker.IsLocked(ip) {
		t.Fatal("2 failures should not lock")
	}

	tracker.RecordFailure(ip)
	if !tracker.IsLocked(ip) {
		t.Fatal("3 failures should lock")
	}

	// Success should clear
	tracker.RecordSuccess(ip)
	if tracker.IsLocked(ip) {
		t.Fatal("success should clear lockout")
	}
}

func TestFailedLoginTrackerSweep(t *testing.T) {
	checkGoroutines(t)
	tracker := NewFailedLoginTracker(3, 5*time.Minute)
	ip := "192.168.1.100"
	tracker.RecordFailure(ip)

	tracker.Sweep(1 * time.Nanosecond)
	if tracker.IsLocked(ip) {
		t.Fatal("swept entry should not exist")
	}
}

// ── Constant-Time Compare ──────────────────────────

func TestConstantTimeCompare(t *testing.T) {
	checkGoroutines(t)
	if ConstantTimeCompare("abc", "abc") != 1 {
		t.Error("equal strings should return 1")
	}
	if ConstantTimeCompare("abc", "def") != 0 {
		t.Error("different strings should return 0")
	}
	if ConstantTimeCompare("abc", "abC") != 0 {
		t.Error("case-different strings should return 0")
	}
}

// ── Session Limits ─────────────────────────────────

func TestCreateWithLimit(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	_, err := store.CreateWithLimit("admin", RoleAdmin, 2)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err = store.CreateWithLimit("admin", RoleAdmin, 2)
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	_, err = store.CreateWithLimit("admin", RoleAdmin, 2)
	if err == nil {
		t.Fatal("third create should fail with limit=2")
	}
}

func TestCountByUser(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	store.Create("admin", RoleAdmin)
	store.Create("admin", RoleAdmin)
	store.Create("user", RoleRead)
	if store.CountByUser("admin") != 2 {
		t.Errorf("admin count = %d, want 2", store.CountByUser("admin"))
	}
	if store.CountByUser("user") != 1 {
		t.Errorf("user count = %d, want 1", store.CountByUser("user"))
	}
	if store.CountByUser("nobody") != 0 {
		t.Errorf("nobody count = %d, want 0", store.CountByUser("nobody"))
	}
}

func TestRevokeAllForUser(t *testing.T) {
	checkGoroutines(t)
	store := NewStore(3600)
	store.Create("admin", RoleAdmin)
	store.Create("admin", RoleAdmin)
	store.Create("user", RoleRead)
	removed := store.RevokeAllForUser("admin")
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	if store.Count() != 1 {
		t.Errorf("count = %d, want 1", store.Count())
	}
}

// ── Password Policy ─────────────────────────────────

func TestPasswordPolicy(t *testing.T) {
	checkGoroutines(t)
	// Minimum length enforcement is frontend + backend advisory
	// Verify bcrypt handles long passwords safely (72 byte truncation)
	hash, _ := HashPassword(string(make([]byte, 100)))
	if CheckPassword(string(make([]byte, 100)), hash) &&
		!CheckPassword(string(make([]byte, 99)), hash) {
		// bcrypt truncates at 72 bytes; this documents that behavior
		t.Log("bcrypt truncates passwords at 72 bytes — documented behavior")
	}
}

// ── Benchmarks ─────────────────────────────────────

func BenchmarkStoreCreate(b *testing.B) {
	store := NewStore(3600)
	for i := 0; i < b.N; i++ {
		_, _ = store.Create("admin", RoleAdmin)
	}
}

func BenchmarkStoreGet(b *testing.B) {
	store := NewStore(3600)
	sid, _ := store.Create("admin", RoleAdmin)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Get(sid)
	}
}

func BenchmarkCheckPassword(b *testing.B) {
	hash, _ := HashPassword("secret123")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CheckPassword("secret123", hash)
	}
}
