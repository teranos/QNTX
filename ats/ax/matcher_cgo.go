//go:build cgo && rustfuzzy

package ax

import (
	"sync"

	"github.com/teranos/QNTX/plugins/qntx-fuzzy/cgo"
)

// To enable CGO fuzzy matching, build with:
//   go build -tags rustfuzzy
// And ensure libqntx_fuzzy is in the library path:
//   export LD_LIBRARY_PATH=/path/to/QNTX/target/release:$LD_LIBRARY_PATH

// CGOMatcher wraps the Rust FuzzyEngine via CGO and implements the Matcher interface.
// It maintains an internal index that is rebuilt when the vocabulary changes.
type CGOMatcher struct {
	engine *cgo.FuzzyEngine

	mu             sync.RWMutex
	predicates     []string
	contexts       []string
	predicateHash  uint64
	contextHash    uint64
}

// NewCGOMatcher creates a new CGO-backed fuzzy matcher.
// Returns an error if the Rust library cannot be loaded.
func NewCGOMatcher() (*CGOMatcher, error) {
	engine, err := cgo.NewFuzzyEngine()
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

// FindMatches finds predicates that match the query using the Rust fuzzy engine.
// The vocabulary is synced to the Rust engine if it has changed.
func (m *CGOMatcher) FindMatches(queryPredicate string, allPredicates []string) []string {
	if queryPredicate == "" {
		return nil
	}

	// Check if we need to rebuild the predicate index
	m.syncPredicates(allPredicates)

	// Query the Rust engine
	matches, err := m.engine.FindPredicateMatches(queryPredicate)
	if err != nil {
		// Fall back to empty on error (could log here)
		return nil
	}

	return matches
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
	matches, err := m.engine.FindContextMatches(queryContext)
	if err != nil {
		return nil
	}

	return matches
}

// syncPredicates rebuilds the predicate index if the vocabulary has changed.
func (m *CGOMatcher) syncPredicates(predicates []string) {
	hash := hashStrings(predicates)

	m.mu.RLock()
	needsRebuild := hash != m.predicateHash
	m.mu.RUnlock()

	if !needsRebuild {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if hash == m.predicateHash {
		return
	}

	// Rebuild index with new predicates, keeping existing contexts
	m.predicates = predicates
	m.predicateHash = hash
	_, _ = m.engine.RebuildIndex(m.predicates, m.contexts)
}

// syncContexts rebuilds the context index if the vocabulary has changed.
func (m *CGOMatcher) syncContexts(contexts []string) {
	hash := hashStrings(contexts)

	m.mu.RLock()
	needsRebuild := hash != m.contextHash
	m.mu.RUnlock()

	if !needsRebuild {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if hash == m.contextHash {
		return
	}

	// Rebuild index with new contexts, keeping existing predicates
	m.contexts = contexts
	m.contextHash = hash
	_, _ = m.engine.RebuildIndex(m.predicates, m.contexts)
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
