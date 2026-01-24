//go:build !rustfuzzy
// +build !rustfuzzy

// Stub implementation when Rust fuzzy-ax library is not available.
// All functions panic - qntx-core is required.

package fuzzyax

import "errors"

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
type FuzzyEngine struct{}

// ErrRustRequired is returned when the Rust library is not available
var ErrRustRequired = errors.New("qntx-core fuzzy engine not available: build with -tags rustfuzzy")

// NewFuzzyEngine creates a new Rust-backed fuzzy matching engine.
func NewFuzzyEngine() (*FuzzyEngine, error) {
	return nil, ErrRustRequired
}

// Close frees the underlying Rust engine.
func (f *FuzzyEngine) Close() error {
	return nil
}

// RebuildIndex rebuilds the fuzzy index with new vocabulary.
func (f *FuzzyEngine) RebuildIndex(predicates, contexts []string) (*RebuildResult, error) {
	return nil, ErrRustRequired
}

// FindMatches finds vocabulary items matching a query.
func (f *FuzzyEngine) FindMatches(query string, vocabType VocabularyType, limit int, minScore float64) ([]Match, uint64, error) {
	return nil, 0, ErrRustRequired
}

// FindPredicateMatches is a convenience method for finding predicate matches.
func (f *FuzzyEngine) FindPredicateMatches(query string) ([]string, error) {
	return nil, ErrRustRequired
}

// FindContextMatches is a convenience method for finding context matches.
func (f *FuzzyEngine) FindContextMatches(query string) ([]string, error) {
	return nil, ErrRustRequired
}

// GetIndexHash returns the current index hash for change detection.
func (f *FuzzyEngine) GetIndexHash() string {
	return ""
}

// IsReady returns true if the index has been built with vocabulary.
func (f *FuzzyEngine) IsReady() bool {
	return false
}

// Version returns the fuzzy-ax library version string.
func Version() string {
	return "stub-rust-required"
}
