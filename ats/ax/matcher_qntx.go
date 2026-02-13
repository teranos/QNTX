//go:build qntxwasm

package ax

import (
	"sync"

	"github.com/teranos/QNTX/ats/wasm"
	"go.uber.org/zap"
)

// WasmMatcher wraps the Rust FuzzyEngine running inside wazero (pure Go WASM).
// Same engine as CGOMatcher but delivered via WASM instead of CGO — no C toolchain needed.
type WasmMatcher struct {
	logger        *zap.SugaredLogger
	predicates    []string
	contexts      []string
	predicateHash uint64
	contextHash   uint64
	mu            sync.RWMutex
}

// NewWasmMatcher creates a new WASM-backed fuzzy matcher.
// Returns an error if the WASM engine cannot be initialized.
func NewWasmMatcher() (*WasmMatcher, error) {
	if _, err := wasm.GetEngine(); err != nil {
		return nil, err
	}
	return &WasmMatcher{}, nil
}

// Backend returns the matcher backend type (WASM implementation)
func (m *WasmMatcher) Backend() MatcherBackend {
	return MatcherBackendWasm
}

// SetLogger sets the logger for debug output
func (m *WasmMatcher) SetLogger(logger interface{}) {
	if l, ok := logger.(*zap.SugaredLogger); ok {
		m.logger = l
	}
}

// FindMatches finds predicates that match the query using the WASM fuzzy engine.
func (m *WasmMatcher) FindMatches(queryPredicate string, allPredicates []string) []string {
	if queryPredicate == "" {
		return nil
	}

	m.syncPredicates(allPredicates)

	engine, err := wasm.GetEngine()
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("wasm engine unavailable", "error", err)
		}
		return nil
	}

	matches, err := engine.FindFuzzyMatches(queryPredicate, "predicates", 20, 0.6)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("wasm fuzzy match failed", "query", queryPredicate, "error", err)
		}
		return nil
	}

	if m.logger != nil && len(matches) > 0 {
		m.logger.Debugw("wasm fuzzy match",
			"query", queryPredicate,
			"matches", len(matches),
			"top_match", matches[0].Value,
			"top_score", matches[0].Score,
			"strategy", matches[0].Strategy,
		)
	}

	values := make([]string, len(matches))
	for i, match := range matches {
		values[i] = match.Value
	}
	return values
}

// FindContextMatches finds contexts that match the query using the WASM fuzzy engine.
func (m *WasmMatcher) FindContextMatches(queryContext string, allContexts []string) []string {
	if queryContext == "" {
		return nil
	}

	m.syncContexts(allContexts)

	engine, err := wasm.GetEngine()
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("wasm engine unavailable", "error", err)
		}
		return nil
	}

	matches, err := engine.FindFuzzyMatches(queryContext, "contexts", 20, 0.6)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("wasm fuzzy context match failed", "query", queryContext, "error", err)
		}
		return nil
	}

	if m.logger != nil && len(matches) > 0 {
		m.logger.Debugw("wasm fuzzy context match",
			"query", queryContext,
			"matches", len(matches),
			"top_match", matches[0].Value,
			"top_score", matches[0].Score,
			"strategy", matches[0].Strategy,
		)
	}

	values := make([]string, len(matches))
	for i, match := range matches {
		values[i] = match.Value
	}
	return values
}

// syncPredicates rebuilds the predicate index if the vocabulary has changed.
func (m *WasmMatcher) syncPredicates(predicates []string) {
	hash := hashStrings(predicates)

	m.mu.RLock()
	needsRebuild := hash != m.predicateHash || len(predicates) != len(m.predicates)
	m.mu.RUnlock()

	if !needsRebuild {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if hash == m.predicateHash && len(predicates) == len(m.predicates) {
		return
	}

	m.predicates = predicates
	m.predicateHash = hash

	engine, err := wasm.GetEngine()
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("wasm engine unavailable for index rebuild", "error", err)
		}
		return
	}

	predCount, ctxCount, indexHash, err := engine.RebuildFuzzyIndex(m.predicates, m.contexts)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("failed to rebuild wasm fuzzy index",
				"predicate_count", len(m.predicates),
				"context_count", len(m.contexts),
				"error", err,
			)
		}
	} else if m.logger != nil {
		m.logger.Debugw("rebuilt wasm fuzzy predicate index",
			"predicate_count", predCount,
			"context_count", ctxCount,
			"index_hash", indexHash,
		)
	}
}

// syncContexts rebuilds the context index if the vocabulary has changed.
func (m *WasmMatcher) syncContexts(contexts []string) {
	hash := hashStrings(contexts)

	m.mu.RLock()
	needsRebuild := hash != m.contextHash || len(contexts) != len(m.contexts)
	m.mu.RUnlock()

	if !needsRebuild {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if hash == m.contextHash && len(contexts) == len(m.contexts) {
		return
	}

	m.contexts = contexts
	m.contextHash = hash

	engine, err := wasm.GetEngine()
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("wasm engine unavailable for index rebuild", "error", err)
		}
		return
	}

	predCount, ctxCount, indexHash, err := engine.RebuildFuzzyIndex(m.predicates, m.contexts)
	if err != nil {
		if m.logger != nil {
			m.logger.Errorw("failed to rebuild wasm fuzzy index",
				"predicate_count", len(m.predicates),
				"context_count", len(m.contexts),
				"error", err,
			)
		}
	} else if m.logger != nil {
		m.logger.Debugw("rebuilt wasm fuzzy context index",
			"predicate_count", predCount,
			"context_count", ctxCount,
			"index_hash", indexHash,
		)
	}
}

// NewDefaultMatcher creates a WASM-backed matcher.
// Panics if the WASM engine is unavailable — run `make wasm`.
func NewDefaultMatcher() Matcher {
	matcher, err := NewWasmMatcher()
	if err != nil {
		panic("WASM engine unavailable for fuzzy matcher: " + err.Error() + " — run `make wasm`")
	}
	return matcher
}

// DetectBackend returns which fuzzy backend is available without
// creating a full matcher instance.
func DetectBackend() MatcherBackend {
	if _, err := NewWasmMatcher(); err == nil {
		return MatcherBackendWasm
	}
	return MatcherBackendGo
}
