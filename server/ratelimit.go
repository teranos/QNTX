package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// ipLimiter tracks a per-IP token bucket and when it was last used.
// lastSeen is stored as UnixNano via atomic to avoid data races between
// concurrent allow() calls and the sweep goroutine.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64 // UnixNano
}

// rateLimitGroup holds per-IP limiters for one category of traffic.
type rateLimitGroup struct {
	limiters sync.Map // map[string]*ipLimiter
	rate     rate.Limit
	burst    int
}

// newRateLimitGroup creates a rate limit group.
func newRateLimitGroup(r float64, burst int) *rateLimitGroup {
	return &rateLimitGroup{
		rate:  rate.Limit(r),
		burst: burst,
	}
}

// allow returns true if the IP has budget remaining.
func (g *rateLimitGroup) allow(ip string) bool {
	now := time.Now()
	entry := &ipLimiter{
		limiter: rate.NewLimiter(g.rate, g.burst),
	}
	entry.lastSeen.Store(now.UnixNano())

	val, loaded := g.limiters.LoadOrStore(ip, entry)
	if loaded {
		// An entry that is not a limiter cannot limit; the fresh one above
		// replaces it rather than waving the request through unmetered.
		existing, isLimiter := val.(*ipLimiter)
		if !isLimiter {
			g.limiters.Store(ip, entry)
		} else {
			entry = existing
		}
		entry.lastSeen.Store(now.UnixNano())
	}
	return entry.limiter.Allow()
}

// sweep removes entries idle longer than maxAge.
func (g *rateLimitGroup) sweep(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge).UnixNano()
	g.limiters.Range(func(key, value any) bool {
		entry, isLimiter := value.(*ipLimiter)
		if !isLimiter || entry.lastSeen.Load() <= cutoff {
			g.limiters.Delete(key)
		}
		return true
	})
}

// clientIP is the address the node treats as the caller: the per-IP rate-limit
// key and the ip field on the access log. It has to be the real remote client,
// because a value the caller can forge lets them evade the per-IP limits or
// wear another caller's address.
//
// The node binds loopback and Caddy is the only thing in front of it
// (infra/bootstrap/Caddyfile, which is frozen to a bare reverse_proxy). Caddy
// appends the address it saw to whatever X-Forwarded-For the caller already
// sent, so the trustworthy client is the last entry, never the first:
// everything to its left is what the caller claimed and can lie about.
//
//   - The direct peer is the trusted proxy (loopback): the client is the
//     rightmost X-Forwarded-For entry, or the peer when the header is absent.
//   - The direct peer is anyone else — a connection that did not come through
//     Caddy: the client is that peer, and X-Forwarded-For is ignored whole,
//     because every hop in it was written by someone untrusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	// Only a request that reached the node through the trusted proxy may name a
	// client other than its own connection. Caddy reaches it over loopback.
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return host
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}

	// The last entry is the one Caddy appended from the connection it
	// terminated. Split on the final comma, not the first.
	last := xff
	if idx := strings.LastIndex(xff, ","); idx != -1 {
		last = xff[idx+1:]
	}
	if last = strings.TrimSpace(last); last != "" {
		return last
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

// authGate rate-limits an auth route and answers it with CORS headers. CORS
// wraps the limiter, so a 429 carries the headers a browser needs to read it
// rather than arriving as a network failure.
func (s *QNTXServer) authGate(handler http.HandlerFunc) http.HandlerFunc {
	return s.corsMiddleware(s.rateLimitAuthMiddleware(handler))
}

// sweepRateLimiters periodically cleans up stale per-IP limiter entries.
// Runs until ctx is cancelled.
func (s *QNTXServer) sweepRateLimiters(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	const maxAge = 15 * time.Minute
	groups := []*rateLimitGroup{s.rlAuth, s.rlWS, s.rlWrite, s.rlRead, s.rlPublic, s.rlStaand, s.rlOpened}

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
