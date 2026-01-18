//go:build !rustfuzzy
// +build !rustfuzzy

// Package fuzzyax provides a stub implementation when Rust library is not available.
package fuzzyax

import (
	"errors"
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
	Text     string
	Score    float64
	Strategy string
}

// FuzzyEngine is a stub implementation that returns no results
type FuzzyEngine struct{}

// NewFuzzyEngine creates a new stub FuzzyEngine
func NewFuzzyEngine() (*FuzzyEngine, error) {
	return &FuzzyEngine{}, nil
}

// RebuildIndex is a no-op in the stub
func (e *FuzzyEngine) RebuildIndex(predicates, contexts []string) int {
	return 0
}

// FindMatches always returns empty results in the stub
func (e *FuzzyEngine) FindMatches(query string, vocabType VocabularyType, limit int, minScore float64) []Match {
	return []Match{}
}

// Close is a no-op in the stub
func (e *FuzzyEngine) Close() error {
	return nil
}

// GetVocabularySize returns 0 in the stub
func (e *FuzzyEngine) GetVocabularySize(vocabType VocabularyType) int {
	return 0
}

// GetStatus returns a stub status
func (e *FuzzyEngine) GetStatus() string {
	return "stub implementation (rustfuzzy build tag not set)"
}

var ErrNotAvailable = errors.New("fuzzy search not available (rustfuzzy build tag not set)")