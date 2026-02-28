package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipLimiter tracks a per-IP token bucket and when it was last used.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// rateLimitGroup holds per-IP limiters for one category of traffic.
type rateLimitGroup struct {
	limiters sync.Map // map[string]*ipLimiter
	rate     rate.Limit
	burst    int
}

// newRateLimitGroup creates a rate limit group. If r is 0, the group is
// disabled and allow() always returns true.
func newRateLimitGroup(r float64, burst int) *rateLimitGroup {
	return &rateLimitGroup{
		rate:  rate.Limit(r),
		burst: burst,
	}
}

// allow returns true if the IP has budget remaining. Disabled groups (rate 0)
// always allow.
func (g *rateLimitGroup) allow(ip string) bool {
	if g.rate == 0 {
		return true
	}

	now := time.Now()
	val, loaded := g.limiters.LoadOrStore(ip, &ipLimiter{
		limiter:  rate.NewLimiter(g.rate, g.burst),
		lastSeen: now,
	})
	entry := val.(*ipLimiter)
	if loaded {
		entry.lastSeen = now
	}
	return entry.limiter.Allow()
}

// sweep removes entries idle longer than maxAge.
func (g *rateLimitGroup) sweep(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	g.limiters.Range(func(key, value any) bool {
		entry := value.(*ipLimiter)
		if entry.lastSeen.Before(cutoff) {
			g.limiters.Delete(key)
		}
		return true
	})
}

// clientIP extracts the client IP from the request. It checks
// X-Forwarded-For (leftmost entry) first, then falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Leftmost IP is the original client
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// denyRateLimit writes a 429 response with Retry-After header.
func denyRateLimit(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
}

// rateLimitMiddleware applies the read or write rate limit group based on HTTP method.
func (s *QNTXServer) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			if !s.rlRead.allow(ip) {
				denyRateLimit(w)
				return
			}
		default:
			if !s.rlWrite.allow(ip) {
				denyRateLimit(w)
				return
			}
		}
		next(w, r)
	}
}

// rateLimitWSMiddleware rate-limits WebSocket upgrade requests.
func (s *QNTXServer) rateLimitWSMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rlWS.allow(clientIP(r)) {
			denyRateLimit(w)
			return
		}
		next(w, r)
	}
}

// rateLimitPublicMiddleware rate-limits public endpoints (/health, static).
func (s *QNTXServer) rateLimitPublicMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rlPublic.allow(clientIP(r)) {
			denyRateLimit(w)
			return
		}
		next(w, r)
	}
}

// rateLimitAuthMiddleware rate-limits authentication endpoints.
func (s *QNTXServer) rateLimitAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.rlAuth.allow(clientIP(r)) {
			denyRateLimit(w)
			return
		}
		next(w, r)
	}
}

// sweepRateLimiters periodically cleans up stale per-IP limiter entries.
// Runs until ctx is cancelled.
func (s *QNTXServer) sweepRateLimiters(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	const maxAge = 15 * time.Minute
	groups := []*rateLimitGroup{s.rlAuth, s.rlWS, s.rlWrite, s.rlRead, s.rlPublic}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			swept := 0
			for _, g := range groups {
				before := countLimiters(g)
				g.sweep(maxAge)
				swept += before - countLimiters(g)
			}
			if swept > 0 {
				s.logger.Debugw("Swept stale rate limiter entries", "removed", swept)
			}
		}
	}
}

// countLimiters returns the number of entries in a rateLimitGroup.
func countLimiters(g *rateLimitGroup) int {
	n := 0
	g.limiters.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// retryAfterSeconds formats a duration as a whole-second string for the
// Retry-After header.
func retryAfterSeconds(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}
