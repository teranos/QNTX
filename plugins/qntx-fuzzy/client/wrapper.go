//go:build grpcfuzzy

// Package client provides a Go client for the qntx-fuzzy Rust service.
//
// RustFuzzyMatcher provides a drop-in replacement for the built-in FuzzyMatcher.
// This package requires the grpcfuzzy build tag and generated proto code.
package client

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RustFuzzyMatcher wraps the Rust fuzzy service client and provides
// the same interface as the built-in ax.FuzzyMatcher.
//
// It maintains vocabulary state and handles synchronization with the Rust service.
type RustFuzzyMatcher struct {
	client *Client
	logger *zap.SugaredLogger

	mu              sync.RWMutex
	predicates      []string
	contexts        []string
	lastVocabUpdate time.Time

	// Configuration
	pollInterval time.Duration
	minScore     float64

	// Fallback to Go implementation if Rust service unavailable
	fallbackEnabled bool
}

// RustFuzzyMatcherConfig configures the Rust fuzzy matcher
type RustFuzzyMatcherConfig struct {
	// ServiceAddress is the gRPC address (e.g., "localhost:9100")
	ServiceAddress string

	// PollInterval for vocabulary refresh
	PollInterval time.Duration

	// MinScore is the minimum match score (0.0-1.0)
	MinScore float64

	// FallbackEnabled enables fallback to Go implementation
	FallbackEnabled bool

	// Logger for debug output
	Logger *zap.SugaredLogger
}

// DefaultRustFuzzyMatcherConfig returns sensible defaults
func DefaultRustFuzzyMatcherConfig() RustFuzzyMatcherConfig {
	return RustFuzzyMatcherConfig{
		ServiceAddress:  "localhost:9100",
		PollInterval:    30 * time.Second,
		MinScore:        0.6,
		FallbackEnabled: true,
	}
}

// NewRustFuzzyMatcher creates a new Rust-backed fuzzy matcher
func NewRustFuzzyMatcher(cfg RustFuzzyMatcherConfig) (*RustFuzzyMatcher, error) {
	client, err := NewClient(Config{
		Address:        cfg.ServiceAddress,
		ConnectTimeout: 5 * time.Second,
		RequestTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		if cfg.FallbackEnabled {
			if cfg.Logger != nil {
				cfg.Logger.Warnw("Failed to connect to Rust fuzzy service, using fallback",
					"error", err,
					"address", cfg.ServiceAddress,
				)
			}
			// Return a matcher that will always use fallback
			return &RustFuzzyMatcher{
				client:          nil,
				logger:          cfg.Logger,
				pollInterval:    cfg.PollInterval,
				minScore:        cfg.MinScore,
				fallbackEnabled: true,
			}, nil
		}
		return nil, err
	}

	return &RustFuzzyMatcher{
		client:          client,
		logger:          cfg.Logger,
		pollInterval:    cfg.PollInterval,
		minScore:        cfg.MinScore,
		fallbackEnabled: cfg.FallbackEnabled,
	}, nil
}

// Close closes the underlying client connection
func (m *RustFuzzyMatcher) Close() error {
	if m.client != nil {
		return m.client.Close()
	}
	return nil
}

// UpdateVocabulary updates the vocabulary in the Rust service
func (m *RustFuzzyMatcher) UpdateVocabulary(ctx context.Context, predicates, contexts []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.predicates = predicates
	m.contexts = contexts
	m.lastVocabUpdate = time.Now()

	if m.client != nil {
		if err := m.client.RebuildIndex(ctx, predicates, contexts); err != nil {
			if m.logger != nil {
				m.logger.Warnw("Failed to update Rust fuzzy index", "error", err)
			}
			// Don't fail - we have the vocabulary locally for fallback
		}
	}

	return nil
}

// FindMatches finds predicates that match the query using fuzzy logic.
// This method signature matches the existing ax.FuzzyMatcher interface.
func (m *RustFuzzyMatcher) FindMatches(queryPredicate string, allPredicates []string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try Rust service first
	if m.client != nil {
		matches, err := m.client.FindPredicateMatches(ctx, queryPredicate)
		if err == nil {
			return matches
		}
		if m.logger != nil {
			m.logger.Debugw("Rust fuzzy match failed, using fallback", "error", err)
		}
	}

	// Fallback to simple Go matching
	return m.fallbackFindMatches(queryPredicate, allPredicates)
}

// FindContextMatches finds contexts that match the query using fuzzy logic.
// This method signature matches the existing ax.FuzzyMatcher interface.
func (m *RustFuzzyMatcher) FindContextMatches(queryContext string, allContexts []string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try Rust service first
	if m.client != nil {
		matches, err := m.client.FindContextMatches(ctx, queryContext)
		if err == nil {
			return matches
		}
		if m.logger != nil {
			m.logger.Debugw("Rust fuzzy context match failed, using fallback", "error", err)
		}
	}

	// Fallback to simple Go matching
	return m.fallbackFindContextMatches(queryContext, allContexts)
}

// fallbackFindMatches provides basic Go-based matching when Rust service unavailable
// This mirrors the logic in ats/ax/fuzzy.go
func (m *RustFuzzyMatcher) fallbackFindMatches(query string, vocabulary []string) []string {
	if len(query) == 0 {
		return nil
	}

	query = toLower(query)
	var matches []string

	for _, item := range vocabulary {
		itemLower := toLower(item)

		// Exact match
		if query == itemLower {
			matches = append(matches, item)
			continue
		}

		// Substring match
		if contains(itemLower, query) {
			matches = append(matches, item)
			continue
		}

		// Word boundary match
		for _, word := range splitWords(itemLower) {
			if word == query {
				matches = append(matches, item)
				break
			}
		}
	}

	return matches
}

// fallbackFindContextMatches provides basic Go-based context matching
func (m *RustFuzzyMatcher) fallbackFindContextMatches(query string, vocabulary []string) []string {
	// Same logic as predicate matching for now
	return m.fallbackFindMatches(query, vocabulary)
}

// Simple string helpers to avoid importing strings package
func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitWords(s string) []string {
	var words []string
	start := -1
	for i, c := range s {
		isSpace := c == ' ' || c == '\t' || c == '\n' || c == '_' || c == '-'
		if isSpace {
			if start >= 0 {
				words = append(words, s[start:i])
				start = -1
			}
		} else {
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		words = append(words, s[start:])
	}
	return words
}
