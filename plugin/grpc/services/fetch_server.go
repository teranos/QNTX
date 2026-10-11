package services

import (
	"bytes"
	"compress/gzip"
	"context"
	"github.com/teranos/QNTX/internal/sqlclose"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
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

// fetchWindow is how many fetches a window allows: some, waited for in turn,
// or none, and why.
type fetchWindow interface {
	// usage is how many requests the window holds now, and how many it allows.
	usage() (current, max int)
	// wait blocks until the window allows a request, or says why it does not.
	wait(ctx context.Context) error
	// length is how long the window is.
	length() time.Duration
}

// closedWindow is a window that allows no request at all, and why.
type closedWindow struct {
	why    error
	window time.Duration
}

func (c closedWindow) usage() (current, max int)  { return 0, 0 }
func (c closedWindow) wait(context.Context) error { return c.why }
func (c closedWindow) length() time.Duration      { return c.window }
func (w *windowLimiter) length() time.Duration    { return w.window }

// newWindowLimiter allows maxReqs a window: "zero means zero", so a window of
// none makes no request.
func newWindowLimiter(maxReqs int, window time.Duration) fetchWindow {
	allowed, err := named.CountOf("fetch.max_requests_per_window", maxReqs)
	if err != nil {
		return closedWindow{why: errors.Wrapf(err, "fetch allows %d requests per window, so none is made", maxReqs), window: window}
	}
	return &windowLimiter{
		times:   make([]time.Time, 0, allowed.Int()),
		maxReqs: allowed.Int(),
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
	// active is a fetch asked for since the last flush, whatever became of it.
	active atomic.Bool
}

func (s *fetchStats) recordRequest(bytes int) {
	s.requests.Add(1)
	s.bytes.Add(int64(bytes))
	s.active.Store(true)
}

func (s *fetchStats) recordError() {
	s.errors.Add(1)
	s.active.Store(true)
}

func (s *fetchStats) recordDedup() {
	s.deduped.Add(1)
	s.active.Store(true)
}

// flush returns accumulated stats and resets counters, and whether any fetch
// was asked for since the last flush.
func (s *fetchStats) flush() (requests, bytes, errors, deduped int64, active bool) {
	active = s.active.Swap(false)
	requests = s.requests.Swap(0)
	bytes = s.bytes.Swap(0)
	errors = s.errors.Swap(0)
	deduped = s.deduped.Swap(0)
	return requests, bytes, errors, deduped, active
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
	node            string // The node's DID: who wrote a fetch a plugin named no actor for.
	client          *http.Client
	logger          *zap.SugaredLogger
	pathLimiter     *rateLimiter
	domainLimiter   *rateLimiter
	globalLimiter   fetchWindow
	stats           fetchStats
	stopPulse       chan struct{}
	versionResolver VersionResolver
	// Set after the service is serving, while plugins may already be calling.
	plugins atomic.Pointer[PluginStores]
}

// SetVersionResolver sets the function used to resolve plugin versions from source names.
func (s *FetchServer) SetVersionResolver(resolver VersionResolver) {
	s.versionResolver = resolver
}

// SetPluginStores hands the server the stores of the namespaces plugins stand in.
func (s *FetchServer) SetPluginStores(plugins PluginStores) {
	s.plugins.Store(&plugins)
}

// storeFor is the store a fetch is attested into: the served one for the
// shared token, the namespace a plugin stands in for that plugin's own token
// (ADR-046), and none for anything else.
func (s *FetchServer) storeFor(token string) (ats.AttestationStore, error) {
	if sameToken(token, s.authToken) {
		return s.store, nil
	}
	if store, standing := (*s.plugins.Load())(token); standing {
		return store, nil
	}
	return nil, errors.New("invalid authentication token")
}

// NewFetchServer takes the fetch config as said: its defaults are am.toml's
// (config.SetDefaults), and a 0 is 0 — no requests in a window, no pulse. node
// is the node's DID.
func NewFetchServer(store ats.AttestationStore, authToken, node string, cfg appcfg.FetchConfig, logger *zap.SugaredLogger) *FetchServer {
	maxReqs := cfg.MaxRequestsPerWindow
	windowSecs := cfg.WindowSeconds
	pulseSecs := cfg.PulseIntervalSeconds

	s := &FetchServer{
		store:     store,
		authToken: authToken,
		node:      node,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:        logger,
		pathLimiter:   newRateLimiter(),
		domainLimiter: newRateLimiter(),
		globalLimiter: newWindowLimiter(maxReqs, time.Duration(windowSecs)*time.Second),
		stopPulse:     make(chan struct{}),

		versionResolver: noVersions,
	}
	s.SetPluginStores(noPluginStanding)
	// "0 ticker interval = no ticking"
	every, err := named.CountOf("fetch.pulse_interval_seconds", pulseSecs)
	if err != nil {
		logger.Infow("Fetch pulse does not tick", "why", err.Error())
		return s
	}
	go s.pulseLoop(time.Duration(every.Int()) * time.Second)
	return s
}

// pulseLoop logs aggregated fetch stats every interval.
func (s *FetchServer) pulseLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			requests, bytes, errors, deduped, active := s.stats.flush()
			if !active {
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
	store, err := s.storeFor(req.AuthToken)
	if err != nil {
		return &protocol.FetchResponse{Success: false, Error: err.Error()}, nil //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
	}

	target, err := named.TextOf("url", req.Url)
	if err != nil {
		return &protocol.FetchResponse{Success: false, Error: err.Error()}, nil //nolint:nilerr // the refusal travels in the response payload
	}

	switch record := req.Record.(type) {
	case *protocol.FetchRequest_Unrecorded:
		if !record.Unrecorded {
			return &protocol.FetchResponse{Success: false, Error: "unrecorded is false: a fetch is attested as it names, or unrecorded"}, nil
		}
		body, statusCode, err := s.fetchLive(ctx, target.String())
		if err != nil {
			return &protocol.FetchResponse{Success: false, Error: err.Error(), StatusCode: int32(statusCode)}, nil //nolint:nilerr // the failure travels in the response payload
		}
		return &protocol.FetchResponse{Success: true, Body: string(body), StatusCode: int32(statusCode)}, nil
	case *protocol.FetchRequest_Attest:
		as, err := s.fetchRecordOf(target.String(), record.Attest)
		if err != nil {
			return &protocol.FetchResponse{Success: false, Error: err.Error()}, nil //nolint:nilerr // the refusal travels in the response payload
		}
		return s.fetchAttested(ctx, store, req.Fresh, as), nil
	}
	return &protocol.FetchResponse{Success: false, Error: "a fetch names what becomes of it: attest, or unrecorded"}, nil
}

// fetchLive is the URL fetched from remote, once the rate limits let it go.
func (s *FetchServer) fetchLive(ctx context.Context, rawURL string) ([]byte, int, error) {
	if err := s.applyRateLimits(ctx, rawURL); err != nil {
		return nil, 0, err
	}
	return s.doHTTPGet(ctx, rawURL)
}

// fetchAttested answers a fetch from its record when one is kept and fresh is
// not asked for, else fetches it and attests what came back.
func (s *FetchServer) fetchAttested(ctx context.Context, store ats.AttestationStore, fresh bool, as fetchRecord) *protocol.FetchResponse {
	if !fresh {
		if resp, found := s.dedupLookup(store, as); found {
			return resp
		}
	}
	body, statusCode, err := s.fetchLive(ctx, as.url)
	if err != nil {
		return &protocol.FetchResponse{Success: false, Error: err.Error(), StatusCode: int32(statusCode)}
	}
	attestID, err := s.attestFetchResult(ctx, store, as, body, statusCode)
	if err != nil {
		// The fetch was made; what is answered is that it is not kept.
		return &protocol.FetchResponse{
			Success:    false,
			Error:      err.Error(),
			Body:       string(body),
			StatusCode: int32(statusCode),
		}
	}
	return &protocol.FetchResponse{
		Success:       true,
		Body:          string(body),
		StatusCode:    int32(statusCode),
		AttestationId: attestID,
	}
}

// fetchRecord is the attestation a fetch is kept as, each part named.
type fetchRecord struct {
	url       string
	subjects  named.Items[string]
	predicate named.Text
	context   named.Text
	actor     named.Text
	source    named.Text
}

// fetchRecordOf reads what a fetch is attested as, or why it cannot be.
func (s *FetchServer) fetchRecordOf(url string, record *protocol.FetchRecord) (fetchRecord, error) {
	subjects, err := named.ItemsOf("attest.subjects", record.GetSubjects())
	if err != nil {
		return fetchRecord{}, err
	}
	predicate, err := named.TextOf("attest.predicate", record.GetPredicate())
	if err != nil {
		return fetchRecord{}, err
	}
	context, err := named.TextOf("attest.context", record.GetContext())
	if err != nil {
		return fetchRecord{}, err
	}
	source, err := named.TextOf("attest.source, the plugin fetching,", record.GetSource())
	if err != nil {
		return fetchRecord{}, err
	}
	actor, err := s.writerOf(record)
	if err != nil {
		return fetchRecord{}, err
	}
	return fetchRecord{url: url, subjects: subjects, predicate: predicate, context: context, actor: actor, source: source}, nil
}

// writerOf is who writes a fetch's attestation: the actor the plugin names, or
// the node, which authors a fetch its plugin asks it to.
func (s *FetchServer) writerOf(record *protocol.FetchRecord) (named.Text, error) {
	switch by := record.GetBy().(type) {
	case *protocol.FetchRecord_Actor:
		return named.TextOf("attest.actor", by.Actor)
	case *protocol.FetchRecord_Node:
		if !by.Node {
			return named.Text{}, errors.New("attest.node is false: a fetch is written by the actor it names, or by the node")
		}
		return named.TextOf("the node's DID", s.node)
	}
	return named.Text{}, errors.New("attest names who writes it: an actor, or the node")
}

// dedupLookup is the record of this URL's fetch, when one is kept: its
// response, and true. False is none kept, and the URL is fetched live.
func (s *FetchServer) dedupLookup(store ats.AttestationStore, as fetchRecord) (*protocol.FetchResponse, bool) {
	results, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Predicates: []string{as.predicate.String()},
		Subjects:   as.subjects.All(),
		Limit:      1,
	})
	if err != nil {
		// A broken store is not a cache miss — the live re-fetch that
		// follows must not hide that the record could not be read.
		s.logger.Warnw("Dedup lookup failed; fetching live instead of answering from the record",
			"url", as.url, "predicate", as.predicate.String(), "error", err)
		return nil, false
	}
	for _, existing := range results {
		if resp, same := s.answeredFrom(existing, as.url); same {
			return resp, true
		}
	}
	return nil, false
}

// answeredFrom is a fetch answered from the record of an earlier one, when
// that record is of the same URL.
func (s *FetchServer) answeredFrom(existing *types.As, url string) (*protocol.FetchResponse, bool) {
	existingURL, ok := existing.Attributes["url"].(string)
	if !ok || existingURL != url {
		return nil, false
	}

	body := ""
	if resp, ok := existing.Attributes["response"].(string); ok {
		body = resp
	}

	// Answer with the status the attestation recorded — the cached fetch may
	// have been a 404 or 500, and a hardcoded 200 tells the caller it wasn't.
	// 0 means the record predates status recording: unknown, not success.
	var statusCode int32
	switch sc := existing.Attributes["status_code"].(type) {
	case float64:
		statusCode = int32(sc)
	case int:
		statusCode = int32(sc)
	}

	s.stats.recordDedup()
	return &protocol.FetchResponse{
		Success:       true,
		Body:          body,
		StatusCode:    statusCode,
		AttestationId: existing.ID,
	}, true
}

// applyRateLimits enforces global, domain, and path rate limits, and says
// which limit refused the fetch, and for which target.
func (s *FetchServer) applyRateLimits(ctx context.Context, rawURL string) error {
	// Global window limit — warn at 80% capacity
	current, max := s.globalLimiter.usage()
	if current >= max*4/5 {
		s.logger.Infow("Fetch window near capacity",
			"current", current,
			"max", max,
			"window", s.globalLimiter.length(),
			"config", "fetch.max_requests_per_window / fetch.window_seconds",
		)
	}
	waitStart := time.Now()
	if err := s.globalLimiter.wait(ctx); err != nil {
		s.logger.Warnw("Fetch dropped: rate limit wait cancelled", "url", rawURL, "error", err)
		return errors.Wrapf(err, "global rate limit wait cancelled for %s", rawURL)
	}
	if waited := time.Since(waitStart); waited > 100*time.Millisecond {
		s.logger.Infow("Fetch throttled by global rate limit", "waited", waited.Round(time.Millisecond), "url", rawURL)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return errors.Wrapf(err, "invalid URL %s", rawURL)
	}

	// Three limiters, three distinct refusals — a caller reading "rate limit
	// wait cancelled" must be able to tell which limit and which target.
	if err := s.domainLimiter.wait(ctx, parsed.Host, 334*time.Millisecond); err != nil {
		s.logger.Warnw("Fetch dropped: domain rate limit wait cancelled", "url", rawURL, "host", parsed.Host, "error", err)
		return errors.Wrapf(err, "domain rate limit wait cancelled for %s", parsed.Host)
	}
	if err := s.pathLimiter.wait(ctx, parsed.Host+parsed.Path, 1*time.Second); err != nil {
		s.logger.Warnw("Fetch dropped: path rate limit wait cancelled", "url", rawURL, "path", parsed.Host+parsed.Path, "error", err)
		return errors.Wrapf(err, "path rate limit wait cancelled for %s", parsed.Host+parsed.Path)
	}

	return nil
}

// doHTTPGet performs the HTTP GET request, reads the body, and handles gzip
// fallback. Returns the body and status code, or why there is none and the
// status code when one came back.
func (s *FetchServer) doHTTPGet(ctx context.Context, rawURL string) ([]byte, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to create request for %s", rawURL)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.stats.recordError()
		return nil, 0, errors.Wrapf(err, "failed to fetch %s", rawURL)
	}
	defer func() { sqlclose.Log(resp.Body.Close(), s.logger, "the fetched response body") }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.stats.recordError()
		return nil, resp.StatusCode, errors.Wrapf(err, "failed to read response body from %s", rawURL)
	}

	// Go's http.Transport strips Content-Encoding: gzip but sometimes fails
	// to decompress, leaving raw gzip bytes in the body. Detect by magic bytes
	// and decompress manually — gzip bytes corrupt string attestation attributes.
	// A body that begins as gzip and does not decompress is refused, not kept
	// as the raw bytes.
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		decompressed, err := gunzip(body)
		if err != nil {
			s.stats.recordError()
			return nil, resp.StatusCode, errors.Wrapf(err, "the body from %s begins as gzip (%d bytes) and does not decompress", rawURL, len(body))
		}
		body = decompressed
	}

	s.stats.recordRequest(len(body))
	return body, resp.StatusCode, nil
}

// gunzip is what gzipped bytes decompress to.
func gunzip(gzipped []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(gzipped))
	if err != nil {
		return nil, err
	}
	decompressed, err := io.ReadAll(gz)
	if err != nil {
		return nil, errors.WithSecondaryError(err, gz.Close())
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return decompressed, nil
}

// attestFetchResult attests the fetch result, and names the attestation.
func (s *FetchServer) attestFetchResult(ctx context.Context, store ats.AttestationStore, as fetchRecord, body []byte, statusCode int) (string, error) {
	predicates := []string{"http:get"}
	if as.predicate.String() != "http:get" {
		predicates = append(predicates, as.predicate.String())
	}

	attrs := map[string]any{
		"url":         as.url,
		"response":    string(body),
		"status_code": statusCode,
	}
	// A fetch is stamped with the version of the plugin that made it. A source
	// the node runs no plugin by has no version to stamp, and is refused.
	version, known := s.versionResolver(as.source.String())
	if !known {
		return "", errors.Newf("%s was fetched for %s, and the node runs no plugin %s, so the registry has no version to stamp it with", as.url, as.source, as.source)
	}
	attrs["source_version"] = version

	cmd := &types.AsCommand{
		Subjects:   as.subjects.All(),
		Predicates: predicates,
		Contexts:   []string{as.context.String()},
		By:         types.ActorNamed(as.actor),
		When:       types.Now{},
		Source:     as.source.String(),
		Attributes: attrs,
	}

	att, err := store.GenerateAndCreateAttestation(ctx, cmd)
	if err != nil {
		s.logger.Warnw("Fetch succeeded but attestation failed", "url", as.url, "error", err)
		return "", errors.Wrapf(err, "%s was fetched, and its attestation was refused", as.url)
	}
	return att.ID, nil
}
