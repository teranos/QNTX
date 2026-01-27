// Package fuzzyax provides a CGO wrapper for the Rust fuzzy matching engine.
//
// This package links directly with the Rust library via CGO, providing
// high-performance fuzzy matching for ax (⋈) queries.
//
// Build Requirements:
//   - Rust toolchain (cargo build --release in ats/ax/fuzzy-ax)
//   - CGO enabled (CGO_ENABLED=1)
//   - Library path set correctly for your platform
//
// Usage:
//
//	engine, err := fuzzyax.NewFuzzyEngine()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer engine.Close()
//
//	engine.RebuildIndex(predicates, contexts)
//	matches := engine.FindMatches("query", fuzzyax.VocabPredicates, 20, 0.6)
package fuzzyax

/*
#cgo CFLAGS: -I${SRCDIR}/../include
#cgo linux LDFLAGS: -L${SRCDIR}/../../../../target/release -lqntx_fuzzy
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../../target/release -lqntx_fuzzy
#cgo windows LDFLAGS: -L${SRCDIR}/../../../../target/release -lqntx_fuzzy

#include "fuzzy_engine.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"runtime"
	"unsafe"

	_ "github.com/teranos/QNTX/ats/internal/cgoflags" // Common system library flags
)

// VocabularyType specifies which vocabulary to search
type VocabularyType int

const (
	// VocabPredicates searches the predicate vocabulary
	VocabPredicates VocabularyType = 0
	// VocabContexts searches the context vocabulary
	VocabContexts VocabularyType = 1
)

// Match represents a fuzzy match result
type Match struct {
	Value    string
	Score    float64
	Strategy string
}

// RebuildResult contains the result of rebuilding the index
type RebuildResult struct {
	PredicateCount int
	ContextCount   int
	BuildTimeMs    uint64
	IndexHash      string
}

// FuzzyEngine wraps the Rust FuzzyEngine via CGO
type FuzzyEngine struct {
	engine *C.FuzzyEngine
}

// NewFuzzyEngine creates a new Rust-backed fuzzy matching engine.
// The caller must call Close() when done to free resources.
func NewFuzzyEngine() (*FuzzyEngine, error) {
	engine := C.fuzzy_engine_new()
	if engine == nil {
		return nil, errors.New("failed to create fuzzy engine")
	}

	fe := &FuzzyEngine{engine: engine}

	// Set finalizer as safety net (but caller should still call Close)
	runtime.SetFinalizer(fe, func(f *FuzzyEngine) {
		f.Close()
	})

	return fe, nil
}

// Close frees the underlying Rust engine.
// Safe to call multiple times.
func (f *FuzzyEngine) Close() error {
	if f.engine != nil {
		C.fuzzy_engine_free(f.engine)
		f.engine = nil
	}
	return nil
}

// RebuildIndex rebuilds the fuzzy index with new vocabulary.
// This should be called when the vocabulary changes.
func (f *FuzzyEngine) RebuildIndex(predicates, contexts []string) (*RebuildResult, error) {
	if f.engine == nil {
		return nil, errors.New("engine is closed")
	}

	// Convert Go slices to C arrays
	var cPredicates **C.char
	var cContexts **C.char

	if len(predicates) > 0 {
		// Use correct type size for pointer allocation
		cPredicates = (**C.char)(C.malloc(C.size_t(len(predicates)) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
		predicateSlice := unsafe.Slice(cPredicates, len(predicates))
		for i, p := range predicates {
			predicateSlice[i] = C.CString(p)
		}
		defer func() {
			for i := 0; i < len(predicates); i++ {
				C.free(unsafe.Pointer(predicateSlice[i]))
			}
			C.free(unsafe.Pointer(cPredicates))
		}()
	}

	if len(contexts) > 0 {
		// Use correct type size for pointer allocation
		cContexts = (**C.char)(C.malloc(C.size_t(len(contexts)) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
		contextSlice := unsafe.Slice(cContexts, len(contexts))
		for i, c := range contexts {
			contextSlice[i] = C.CString(c)
		}
		defer func() {
			for i := 0; i < len(contexts); i++ {
				C.free(unsafe.Pointer(contextSlice[i]))
			}
			C.free(unsafe.Pointer(cContexts))
		}()
	}

	result := C.fuzzy_engine_rebuild_index(
		f.engine,
		cPredicates,
		C.size_t(len(predicates)),
		cContexts,
		C.size_t(len(contexts)),
	)
	defer C.fuzzy_rebuild_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.New(errMsg)
	}

	return &RebuildResult{
		PredicateCount: int(result.predicate_count),
		ContextCount:   int(result.context_count),
		BuildTimeMs:    uint64(result.build_time_ms),
		IndexHash:      C.GoString(result.index_hash),
	}, nil
}

// FindMatches finds vocabulary items matching a query.
//
// Parameters:
//   - query: The search query
//   - vocabType: VocabPredicates or VocabContexts
//   - limit: Maximum results (0 for default of 20)
//   - minScore: Minimum score 0.0-1.0 (0 for default of 0.6)
//
// Returns matches sorted by score descending and search time in microseconds.
func (f *FuzzyEngine) FindMatches(query string, vocabType VocabularyType, limit int, minScore float64) ([]Match, uint64, error) {
	if f.engine == nil {
		return nil, 0, errors.New("engine is closed")
	}

	cQuery := C.CString(query)
	defer C.free(unsafe.Pointer(cQuery))

	result := C.fuzzy_engine_find_matches(
		f.engine,
		cQuery,
		C.int(vocabType),
		C.size_t(limit),
		C.double(minScore),
	)
	defer C.fuzzy_match_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, 0, errors.New(errMsg)
	}

	if result.matches_len == 0 {
		return nil, uint64(result.search_time_us), nil
	}

	// Convert C matches to Go
	matches := make([]Match, result.matches_len)
	cMatches := unsafe.Slice(result.matches, result.matches_len)

	for i, cm := range cMatches {
		matches[i] = Match{
			Value:    C.GoString(cm.value),
			Score:    float64(cm.score),
			Strategy: C.GoString(cm.strategy),
		}
	}

	return matches, uint64(result.search_time_us), nil
}

// FindPredicateMatches is a convenience method for finding predicate matches.
// Returns just the matched values (not scores/strategies).
func (f *FuzzyEngine) FindPredicateMatches(query string) ([]string, error) {
	matches, _, err := f.FindMatches(query, VocabPredicates, 20, 0.6)
	if err != nil {
		return nil, err
	}

	values := make([]string, len(matches))
	for i, m := range matches {
		values[i] = m.Value
	}
	return values, nil
}

// FindContextMatches is a convenience method for finding context matches.
// Returns just the matched values (not scores/strategies).
func (f *FuzzyEngine) FindContextMatches(query string) ([]string, error) {
	matches, _, err := f.FindMatches(query, VocabContexts, 20, 0.6)
	if err != nil {
		return nil, err
	}

	values := make([]string, len(matches))
	for i, m := range matches {
		values[i] = m.Value
	}
	return values, nil
}

// GetIndexHash returns the current index hash for change detection.
func (f *FuzzyEngine) GetIndexHash() string {
	if f.engine == nil {
		return ""
	}

	cHash := C.fuzzy_engine_get_hash(f.engine)
	if cHash == nil {
		return ""
	}
	defer C.fuzzy_string_free(cHash)

	return C.GoString(cHash)
}

// IsReady returns true if the index has been built with vocabulary.
func (f *FuzzyEngine) IsReady() bool {
	if f.engine == nil {
		return false
	}
	return bool(C.fuzzy_engine_is_ready(f.engine))
}

// Version returns the fuzzy-ax library version string.
func Version() string {
	return C.GoString(C.fuzzy_engine_version())
}
