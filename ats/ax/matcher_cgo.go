//go:build cgo && rustfuzzy

package ax

import (
	"sync"

	"github.com/teranos/QNTX/ats/ax/fuzzy-ax/fuzzyax"
	"go.uber.org/zap"
)

// To enable CGO fuzzy matching, build with:
//   go build -tags rustfuzzy
// And ensure libqntx_fuzzy is in the library path:
//   export LD_LIBRARY_PATH=/path/to/QNTX/ats/ax/fuzzy-ax/target/release:$LD_LIBRARY_PATH

// CGOMatcher wraps the Rust FuzzyEngine via CGO and implements the Matcher interface.
// It maintains an internal index that is rebuilt when the vocabulary changes.
type CGOMatcher struct {
	engine *fuzzyax.FuzzyEngine
	logger *zap.SugaredLogger

	mu            sync.RWMutex
	predicates    []string
	contexts      []string
	predicateHash uint64
	contextHash   uint64
}

// NewCGOMatcher creates a new CGO-backed fuzzy matcher.
// Returns an error if the Rust library cannot be loaded.
func NewCGOMatcher() (*CGOMatcher, error) {
	engine, err := fuzzyax.NewFuzzyEngine()
	if err != nil {
		return nil, err
	}

	return &CGOMatcher{
		engine: engine,
	}, nil
}

// Close releases the underlying Rust engine resources.
func (m *CGOMatcher) Close() error {
	if m.engine != nil {
		return m.engine.Close()
	}
	return nil
}

// Backend returns the matcher backend type (Rust implementation via CGO)
func (m *CGOMatcher) Backend() MatcherBackend {
	return MatcherBackendRust
}

// SetLogger sets the logger for debug output
func (m *CGOMatcher) SetLogger(logger interface{}) {
	if l, ok := logger.(*zap.SugaredLogger); ok {
		m.logger = l
	}
}

// FindMatches finds predicates that match the query using the Rust fuzzy engine.
// The vocabulary is synced to the Rust engine if it has changed.
func (m *CGOMatcher) FindMatches(queryPredicate string, allPredicates []string) []string {
	if queryPredicate == "" {
		return nil
	}

	// Check if we need to rebuild the predicate index
	m.syncPredicates(allPredicates)

	// Query the Rust engine
	matches, searchTimeUs, err := m.engine.FindMatches(queryPredicate, fuzzyax.VocabPredicates, 20, 0.6)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("rust fuzzy match failed",
				"query", queryPredicate,
				"error", err,
			)
		}
		return nil
	}

	if m.logger != nil && len(matches) > 0 {
		m.logger.Debugw("rust fuzzy match",
			"query", queryPredicate,
			"matches", len(matches),
			"time_us", searchTimeUs,
			"top_match", matches[0].Value,
			"top_score", matches[0].Score,
			"strategy", matches[0].Strategy,
		)
	}

	// Extract just the values
	values := make([]string, len(matches))
	for i, match := range matches {
		values[i] = match.Value
	}
	return values
}

// FindContextMatches finds contexts that match the query using the Rust fuzzy engine.
// The vocabulary is synced to the Rust engine if it has changed.
func (m *CGOMatcher) FindContextMatches(queryContext string, allContexts []string) []string {
	if queryContext == "" {
		return nil
	}

	// Check if we need to rebuild the context index
	m.syncContexts(allContexts)

	// Query the Rust engine
	matches, searchTimeUs, err := m.engine.FindMatches(queryContext, fuzzyax.VocabContexts, 20, 0.6)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("rust fuzzy context match failed",
				"query", queryContext,
				"error", err,
			)
		}
		return nil
	}

	if m.logger != nil && len(matches) > 0 {
		m.logger.Debugw("rust fuzzy context match",
			"query", queryContext,
			"matches", len(matches),
			"time_us", searchTimeUs,
			"top_match", matches[0].Value,
			"top_score", matches[0].Score,
			"strategy", matches[0].Strategy,
		)
	}

	// Extract just the values
	values := make([]string, len(matches))
	for i, match := range matches {
		values[i] = match.Value
	}
	return values
}

// syncPredicates rebuilds the predicate index if the vocabulary has changed.
func (m *CGOMatcher) syncPredicates(predicates []string) {
	hash := hashStrings(predicates)

	m.mu.RLock()
	// Check both hash and length to avoid hash collisions
	needsRebuild := hash != m.predicateHash || len(predicates) != len(m.predicates)
	m.mu.RUnlock()

	if !needsRebuild {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if hash == m.predicateHash && len(predicates) == len(m.predicates) {
		return
	}

	// Rebuild index with new predicates, keeping existing contexts
	m.predicates = predicates
	m.predicateHash = hash

	result, err := m.engine.RebuildIndex(m.predicates, m.contexts)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("failed to rebuild rust fuzzy index",
				"predicate_count", len(m.predicates),
				"context_count", len(m.contexts),
				"error", err,
			)
		}
	} else if m.logger != nil {
		m.logger.Debugw("rebuilt rust fuzzy predicate index",
			"predicate_count", result.PredicateCount,
			"context_count", result.ContextCount,
			"build_time_ms", result.BuildTimeMs,
			"index_hash", result.IndexHash,
		)
	}
}

// syncContexts rebuilds the context index if the vocabulary has changed.
func (m *CGOMatcher) syncContexts(contexts []string) {
	hash := hashStrings(contexts)

	m.mu.RLock()
	// Check both hash and length to avoid hash collisions
	needsRebuild := hash != m.contextHash || len(contexts) != len(m.contexts)
	m.mu.RUnlock()

	if !needsRebuild {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if hash == m.contextHash && len(contexts) == len(m.contexts) {
		return
	}

	// Rebuild index with new contexts, keeping existing predicates
	m.contexts = contexts
	m.contextHash = hash

	result, err := m.engine.RebuildIndex(m.predicates, m.contexts)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("failed to rebuild rust fuzzy index",
				"predicate_count", len(m.predicates),
				"context_count", len(m.contexts),
				"error", err,
			)
		}
	} else if m.logger != nil {
		m.logger.Debugw("rebuilt rust fuzzy context index",
			"predicate_count", result.PredicateCount,
			"context_count", result.ContextCount,
			"build_time_ms", result.BuildTimeMs,
			"index_hash", result.IndexHash,
		)
	}
}

// hashStrings computes a simple hash of a string slice for change detection.
func hashStrings(strs []string) uint64 {
	var hash uint64 = 14695981039346656037 // FNV-1a offset basis
	for _, s := range strs {
		for i := 0; i < len(s); i++ {
			hash ^= uint64(s[i])
			hash *= 1099511628211 // FNV-1a prime
		}
		hash ^= uint64(0xff) // separator
		hash *= 1099511628211
	}
	return hash
}

// Ensure CGOMatcher implements Matcher
var _ Matcher = (*CGOMatcher)(nil)
