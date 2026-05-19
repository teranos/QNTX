package grpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// windowLimiter enforces a maximum number of requests per rolling window.
// When the window is full, wait blocks until the oldest request expires.
type windowLimiter struct {
	mu      sync.Mutex
	times   []time.Time
	maxReqs int
	window  time.Duration
}

func newWindowLimiter(maxReqs int, window time.Duration) *windowLimiter {
	return &windowLimiter{
		times:   make([]time.Time, 0, maxReqs),
		maxReqs: maxReqs,
		window:  window,
	}
}

// usage returns current count and max.
func (w *windowLimiter) usage() (current, max int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-w.window)
	start := 0
	for start < len(w.times) && w.times[start].Before(cutoff) {
		start++
	}
	return len(w.times) - start, w.maxReqs
}

// wait blocks until a slot is available in the window, or ctx is cancelled.
func (w *windowLimiter) wait(ctx context.Context) error {
	for {
		w.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-w.window)
		// Trim expired entries
		start := 0
		for start < len(w.times) && w.times[start].Before(cutoff) {
			start++
		}
		w.times = w.times[start:]
		if len(w.times) < w.maxReqs {
			w.times = append(w.times, now)
			w.mu.Unlock()
			return nil
		}
		// Wait until the oldest request expires
		delay := w.times[0].Add(w.window).Sub(now)
		w.mu.Unlock()
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// fetchStats accumulates fetch metrics for periodic logging.
type fetchStats struct {
	requests atomic.Int64
	bytes    atomic.Int64
	errors   atomic.Int64
	deduped  atomic.Int64
}

func (s *fetchStats) recordRequest(bytes int) {
	s.requests.Add(1)
	s.bytes.Add(int64(bytes))
}

func (s *fetchStats) recordError() {
	s.errors.Add(1)
}

func (s *fetchStats) recordDedup() {
	s.deduped.Add(1)
}

// flush returns accumulated stats and resets counters. Returns false if no activity.
func (s *fetchStats) flush() (requests, bytes, errors, deduped int64, ok bool) {
	requests = s.requests.Swap(0)
	bytes = s.bytes.Swap(0)
	errors = s.errors.Swap(0)
	deduped = s.deduped.Swap(0)
	return requests, bytes, errors, deduped, requests > 0 || deduped > 0
}

// rateLimiter tracks last-request times per key and enforces minimum intervals.
type rateLimiter struct {
	mu       sync.Mutex
	lastSeen map[string]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{lastSeen: make(map[string]time.Time)}
}

// wait blocks until the minimum interval has elapsed since the last request for this key.
func (r *rateLimiter) wait(ctx context.Context, key string, minInterval time.Duration) error {
	r.mu.Lock()
	last := r.lastSeen[key]
	now := time.Now()
	elapsed := now.Sub(last)
	if elapsed < minInterval {
		delay := minInterval - elapsed
		r.lastSeen[key] = now.Add(delay)
		r.mu.Unlock()
		select {
		case <-time.After(delay):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.lastSeen[key] = now
	r.mu.Unlock()
	return nil
}

// FetchServer implements the FetchService gRPC server.
// Performs HTTP GET requests on behalf of plugins and attests the response.
// Rate limits: 1 req/s per path, 3 req/s per domain.
type FetchServer struct {
	protocol.UnimplementedFetchServiceServer
	store           ats.AttestationStore
	authToken       string
	client          *http.Client
	logger          *zap.SugaredLogger
	pathLimiter     *rateLimiter
	domainLimiter   *rateLimiter
	globalLimiter   *windowLimiter
	stats           fetchStats
	stopPulse       chan struct{}
	versionResolver VersionResolver
}

// SetVersionResolver sets the function used to resolve plugin versions from source names.
func (s *FetchServer) SetVersionResolver(resolver VersionResolver) {
	s.versionResolver = resolver
}

func NewFetchServer(store ats.AttestationStore, authToken string, cfg appcfg.FetchConfig, logger *zap.SugaredLogger) *FetchServer {
	maxReqs := cfg.MaxRequestsPerWindow
	if maxReqs <= 0 {
		maxReqs = 100
	}
	windowSecs := cfg.WindowSeconds
	if windowSecs <= 0 {
		windowSecs = 300
	}
	pulseSecs := cfg.PulseIntervalSeconds
	if pulseSecs <= 0 {
		pulseSecs = 30
	}

	s := &FetchServer{
		store:     store,
		authToken: authToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:        logger,
		pathLimiter:   newRateLimiter(),
		domainLimiter: newRateLimiter(),
		globalLimiter: newWindowLimiter(maxReqs, time.Duration(windowSecs)*time.Second),
		stopPulse:     make(chan struct{}),
	}
	go s.pulseLoop(time.Duration(pulseSecs) * time.Second)
	return s
}

// pulseLoop logs aggregated fetch stats every interval.
func (s *FetchServer) pulseLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			requests, bytes, errors, deduped, ok := s.stats.flush()
			if !ok {
				continue
			}
			s.logger.Infow("Fetch pulse",
				"requests", requests,
				"bytes", bytes,
				"errors", errors,
				"deduped", deduped,
			)
		case <-s.stopPulse:
			return
		}
	}
}

// Stop cleanly shuts down the pulse logger.
func (s *FetchServer) Stop() {
	close(s.stopPulse)
}

func (s *FetchServer) Fetch(ctx context.Context, req *protocol.FetchRequest) (*protocol.FetchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.FetchResponse{Success: false, Error: err.Error()}, nil
	}

	if req.Url == "" {
		return &protocol.FetchResponse{Success: false, Error: "url is required"}, nil
	}

	// Dedup: if we already have an attestation for this URL, return it (unless fresh requested).
	// Query by predicate + subjects (not URL context) so dedup works regardless of attestation context.
	if req.Predicate != "" && !req.Fresh && len(req.Subjects) > 0 {
		results, err := s.store.GetAttestations(ats.AttestationFilter{
			Predicates: []string{req.Predicate},
			Subjects:   req.Subjects,
			Limit:      1,
		})
		if err == nil && len(results) > 0 {
			existing := results[0]
			// Verify URL matches to avoid returning wrong cached response
			if existingURL, ok := existing.Attributes["url"].(string); ok && existingURL == req.Url {
				body := ""
				if resp, ok := existing.Attributes["response"].(string); ok {
					body = resp
				}
				s.stats.recordDedup()
				return &protocol.FetchResponse{
					Success:       true,
					Body:          body,
					StatusCode:    200,
					AttestationId: existing.ID,
				}, nil
			}
		}
	}

	// Global rate limit — warn at 80% capacity, warn when blocking
	current, max := s.globalLimiter.usage()
	if current >= max*4/5 {
		s.logger.Warnw("Fetch window near capacity",
			"current", current,
			"max", max,
			"window", s.globalLimiter.window,
			"config", "fetch.max_requests_per_window / fetch.window_seconds",
		)
	}
	waitStart := time.Now()
	if err := s.globalLimiter.wait(ctx); err != nil {
		s.logger.Warnw("Fetch dropped: rate limit wait cancelled", "url", req.Url, "error", err)
		return &protocol.FetchResponse{Success: false, Error: fmt.Sprintf("global rate limit wait cancelled: %v", err)}, nil
	}
	if waited := time.Since(waitStart); waited > 100*time.Millisecond {
		s.logger.Warnw("Fetch throttled by global rate limit", "waited", waited.Round(time.Millisecond), "url", req.Url)
	}

	// Rate limit: 1 req/s per path, 3 req/s (333ms) per domain
	parsed, err := url.Parse(req.Url)
	if err != nil {
		return &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid URL %s: %v", req.Url, err),
		}, nil
	}
	domain := parsed.Host
	path := parsed.Host + parsed.Path

	if err := s.domainLimiter.wait(ctx, domain, 334*time.Millisecond); err != nil {
		return &protocol.FetchResponse{Success: false, Error: fmt.Sprintf("rate limit wait cancelled: %v", err)}, nil
	}
	if err := s.pathLimiter.wait(ctx, path, 1*time.Second); err != nil {
		return &protocol.FetchResponse{Success: false, Error: fmt.Sprintf("rate limit wait cancelled: %v", err)}, nil
	}

	// HTTP GET
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.Url, nil)
	if err != nil {
		return &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create request for %s: %v", req.Url, err),
		}, nil
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.stats.recordError()
		return &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to fetch %s: %v", req.Url, err),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.stats.recordError()
		return &protocol.FetchResponse{
			Success:    false,
			Error:      fmt.Sprintf("failed to read response body from %s: %v", req.Url, err),
			StatusCode: int32(resp.StatusCode),
		}, nil
	}

	s.stats.recordRequest(len(body))

	// Attest the result.
	// Context comes from the caller (workflow context like "ic"), not the URL.
	// URL is stored in attributes for dedup and reference.
	attestID := ""
	if len(req.Subjects) > 0 && req.Predicate != "" {
		attCtx := req.Context
		if attCtx == "" {
			attCtx = req.Url
		}

		predicates := []string{"http:get"}
		if req.Predicate != "http:get" {
			predicates = append(predicates, req.Predicate)
		}

		actor := req.Actor
		if actor == "" {
			actor = "fetch-service"
		}

		source := req.Source
		if source == "" {
			source = "fetch-service"
		}

		attrs := map[string]interface{}{
			"url":         req.Url,
			"response":    string(body),
			"status_code": resp.StatusCode,
		}
		if s.versionResolver != nil {
			if v := s.versionResolver(source); v != "" {
				attrs["source_version"] = v
			}
		}

		cmd := &types.AsCommand{
			Subjects:   req.Subjects,
			Predicates: predicates,
			Contexts:   []string{attCtx},
			Actors:     []string{actor},
			Source:     source,
			Attributes: attrs,
		}

		att, err := s.store.GenerateAndCreateAttestation(ctx, cmd)
		if err != nil {
			s.logger.Warnw("Fetch succeeded but attestation failed",
				"url", req.Url,
				"error", err,
			)
		} else {
			attestID = att.ID
		}
	}

	return &protocol.FetchResponse{
		Success:       true,
		Body:          string(body),
		StatusCode:    int32(resp.StatusCode),
		AttestationId: attestID,
	}, nil
}
