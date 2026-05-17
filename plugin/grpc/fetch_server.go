package grpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

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
	store       ats.AttestationStore
	authToken   string
	client      *http.Client
	logger      *zap.SugaredLogger
	pathLimiter   *rateLimiter
	domainLimiter *rateLimiter
}

func NewFetchServer(store ats.AttestationStore, authToken string, logger *zap.SugaredLogger) *FetchServer {
	return &FetchServer{
		store:     store,
		authToken: authToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger:        logger,
		pathLimiter:   newRateLimiter(),
		domainLimiter: newRateLimiter(),
	}
}

func (s *FetchServer) Fetch(ctx context.Context, req *protocol.FetchRequest) (*protocol.FetchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.FetchResponse{Success: false, Error: err.Error()}, nil
	}

	if req.Url == "" {
		return &protocol.FetchResponse{Success: false, Error: "url is required"}, nil
	}

	// Dedup: if we already have an attestation for this URL, return it (unless fresh requested)
	if req.Predicate != "" && !req.Fresh {
		results, err := s.store.GetAttestations(ats.AttestationFilter{
			Predicates: []string{req.Predicate},
			Contexts:   []string{req.Url},
			Limit:      1,
		})
		if err == nil && len(results) > 0 {
			existing := results[0]
			body := ""
			if resp, ok := existing.Attributes["response"].(string); ok {
				body = resp
			}
			s.logger.Debugw("Fetch dedup: returning cached attestation",
				"url", req.Url,
				"attestation_id", existing.ID,
			)
			return &protocol.FetchResponse{
				Success:       true,
				Body:          body,
				StatusCode:    200,
				AttestationId: existing.ID,
			}, nil
		}
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
		return &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to fetch %s: %v", req.Url, err),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &protocol.FetchResponse{
			Success:    false,
			Error:      fmt.Sprintf("failed to read response body from %s: %v", req.Url, err),
			StatusCode: int32(resp.StatusCode),
		}, nil
	}

	s.logger.Infow("Fetch completed",
		"url", req.Url,
		"status", resp.StatusCode,
		"bytes", len(body),
		"subjects", req.Subjects,
	)

	// Attest the result
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

		cmd := &types.AsCommand{
			Subjects:   req.Subjects,
			Predicates: predicates,
			Contexts:   []string{attCtx},
			Actors:     []string{"voor:pipeline"},
			Source:     "fetch-service",
			Attributes: map[string]interface{}{
				"response":    string(body),
				"status_code": resp.StatusCode,
			},
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
