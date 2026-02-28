package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitGroup_AllowAndDeny(t *testing.T) {
	g := newRateLimitGroup(1, 3) // 1/sec, burst 3

	// First 3 requests should pass (burst)
	for i := 0; i < 3; i++ {
		if !g.allow("10.0.0.1") {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}

	// 4th request should be denied (burst exhausted)
	if g.allow("10.0.0.1") {
		t.Fatal("request should have been denied after burst exhausted")
	}

	// Different IP should still be allowed (per-IP isolation)
	if !g.allow("10.0.0.2") {
		t.Fatal("different IP should be allowed")
	}
}

func TestRateLimitGroup_DisabledWhenRateZero(t *testing.T) {
	g := newRateLimitGroup(0, 0)

	for i := 0; i < 100; i++ {
		if !g.allow("10.0.0.1") {
			t.Fatal("disabled group should always allow")
		}
	}
}

func TestRateLimitGroup_Sweep(t *testing.T) {
	g := newRateLimitGroup(10, 20)

	g.allow("10.0.0.1")
	g.allow("10.0.0.2")

	if n := countLimiters(g); n != 2 {
		t.Fatalf("expected 2 limiters, got %d", n)
	}

	// Sweep with zero maxAge should remove all entries
	g.sweep(0)

	if n := countLimiters(g); n != 0 {
		t.Fatalf("expected 0 limiters after sweep, got %d", n)
	}
}

func TestRateLimitGroup_SweepKeepsFresh(t *testing.T) {
	g := newRateLimitGroup(10, 20)

	g.allow("10.0.0.1")

	// Sweep with large maxAge should keep the entry
	g.sweep(time.Hour)

	if n := countLimiters(g); n != 1 {
		t.Fatalf("expected 1 limiter (fresh), got %d", n)
	}
}

func TestClientIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.1:12345"

	if ip := clientIP(r); ip != "192.168.1.1" {
		t.Fatalf("expected 192.168.1.1, got %s", ip)
	}
}

func TestClientIP_XForwardedForSingle(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.50")

	if ip := clientIP(r); ip != "203.0.113.50" {
		t.Fatalf("expected 203.0.113.50, got %s", ip)
	}
}

func TestClientIP_XForwardedForChain(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18, 150.172.238.178")

	if ip := clientIP(r); ip != "203.0.113.50" {
		t.Fatalf("expected leftmost IP 203.0.113.50, got %s", ip)
	}
}

func TestRateLimitMiddleware_Returns429(t *testing.T) {
	s := &QNTXServer{
		rlRead:  newRateLimitGroup(1, 1), // 1/sec, burst 1
		rlWrite: newRateLimitGroup(1, 1),
	}

	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// First request: allowed
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	// Second request: rate limited
	rec = httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", rec.Code)
	}

	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on 429 response")
	}
}

func TestRateLimitMiddleware_WriteMethod(t *testing.T) {
	s := &QNTXServer{
		rlRead:  newRateLimitGroup(100, 200), // generous read limit
		rlWrite: newRateLimitGroup(1, 1),     // tight write limit
	}

	handler := s.rateLimitMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// POST uses write limiter
	req := httptest.NewRequest("POST", "/api/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first POST: expected 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second POST: expected 429, got %d", rec.Code)
	}
}
