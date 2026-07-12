package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
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

	// Dedup: return cached attestation if we already fetched this URL
	if resp, found := s.dedupLookup(req); found {
		return resp, nil
	}

	if resp, err := s.applyRateLimits(ctx, req.Url); err != nil {
		return resp, nil
	}

	body, statusCode, fetchErr := s.doHTTPGet(ctx, req.Url)
	if fetchErr != nil {
		return fetchErr, nil
	}

	attestID := s.attestFetchResult(ctx, req, body, statusCode)

	return &protocol.FetchResponse{
		Success:       true,
		Body:          string(body),
		StatusCode:    int32(statusCode),
		AttestationId: attestID,
	}, nil
}

// dedupLookup checks if we already have an attestation for this URL.
// Returns the cached response and true if found, nil and false otherwise.
func (s *FetchServer) dedupLookup(req *protocol.FetchRequest) (*protocol.FetchResponse, bool) {
	if req.Predicate == "" || req.Fresh || len(req.Subjects) == 0 {
		return nil, false
	}

	results, err := s.store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{req.Predicate},
		Subjects:   req.Subjects,
		Limit:      1,
	})
	if err != nil || len(results) == 0 {
		return nil, false
	}

	existing := results[0]
	existingURL, ok := existing.Attributes["url"].(string)
	if !ok || existingURL != req.Url {
		return nil, false
	}

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
	}, true
}

// applyRateLimits enforces global, domain, and path rate limits.
// Returns an error response if rate limiting fails, nil otherwise.
func (s *FetchServer) applyRateLimits(ctx context.Context, rawURL string) (*protocol.FetchResponse, error) {
	// Global window limit — warn at 80% capacity
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
		s.logger.Warnw("Fetch dropped: rate limit wait cancelled", "url", rawURL, "error", err)
		return &protocol.FetchResponse{Success: false, Error: fmt.Sprintf("global rate limit wait cancelled: %v", err)}, fmt.Errorf("cancelled")
	}
	if waited := time.Since(waitStart); waited > 100*time.Millisecond {
		s.logger.Warnw("Fetch throttled by global rate limit", "waited", waited.Round(time.Millisecond), "url", rawURL)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return &protocol.FetchResponse{Success: false, Error: fmt.Sprintf("invalid URL %s: %v", rawURL, err)}, fmt.Errorf("invalid URL")
	}

	if err := s.domainLimiter.wait(ctx, parsed.Host, 334*time.Millisecond); err != nil {
		return &protocol.FetchResponse{Success: false, Error: fmt.Sprintf("rate limit wait cancelled: %v", err)}, fmt.Errorf("cancelled")
	}
	if err := s.pathLimiter.wait(ctx, parsed.Host+parsed.Path, 1*time.Second); err != nil {
		return &protocol.FetchResponse{Success: false, Error: fmt.Sprintf("rate limit wait cancelled: %v", err)}, fmt.Errorf("cancelled")
	}

	return nil, nil
}

// doHTTPGet performs the HTTP GET request, reads the body, and handles gzip fallback.
// Returns body bytes, status code, or an error response.
func (s *FetchServer) doHTTPGet(ctx context.Context, rawURL string) ([]byte, int, *protocol.FetchResponse) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create request for %s: %v", rawURL, err),
		}
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.stats.recordError()
		return nil, 0, &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to fetch %s: %v", rawURL, err),
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.stats.recordError()
		return nil, resp.StatusCode, &protocol.FetchResponse{
			Success:    false,
			Error:      fmt.Sprintf("failed to read response body from %s: %v", rawURL, err),
			StatusCode: int32(resp.StatusCode),
		}
	}

	// Go's http.Transport strips Content-Encoding: gzip but sometimes fails
	// to decompress, leaving raw gzip bytes in the body. Detect by magic bytes
	// and decompress manually — gzip bytes corrupt string attestation attributes.
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		gz, gzErr := gzip.NewReader(bytes.NewReader(body))
		if gzErr == nil {
			if decompressed, readErr := io.ReadAll(gz); readErr == nil {
				body = decompressed
			}
			gz.Close()
		}
	}

	s.stats.recordRequest(len(body))
	return body, resp.StatusCode, nil
}

// attestFetchResult creates an attestation for the fetch result.
// Returns the attestation ID, or empty string if attestation was skipped or failed.
func (s *FetchServer) attestFetchResult(ctx context.Context, req *protocol.FetchRequest, body []byte, statusCode int) string {
	if len(req.Subjects) == 0 || req.Predicate == "" {
		return ""
	}

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

	attrs := map[string]any{
		"url":         req.Url,
		"response":    string(body),
		"status_code": statusCode,
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
		return ""
	}
	return att.ID
}
