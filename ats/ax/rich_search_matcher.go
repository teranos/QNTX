package ax

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ax/fuzzy-ax/fuzzyax"
)

// FuzzyRichSearchMatcher implements ats.RichSearchMatcher using the Rust fuzzy engine.
// Each call to MatchWords creates a short-lived engine, indexes the vocabulary, and
// finds matches — callers don't manage engine lifecycle.
type FuzzyRichSearchMatcher struct{}

// NewFuzzyRichSearchMatcher creates a RichSearchMatcher backed by the Rust fuzzy engine.
// Returns nil if the Rust backend is not available.
func NewFuzzyRichSearchMatcher() ats.RichSearchMatcher {
	if DetectBackend() != MatcherBackendRust {
		return nil
	}
	return &FuzzyRichSearchMatcher{}
}

// MatchWords finds fuzzy matches for each query word against the vocabulary.
func (m *FuzzyRichSearchMatcher) MatchWords(vocabulary []string, queryWords []string, limit int, minScore float64) (map[string][]ats.WordMatch, error) {
	engine, err := fuzzyax.NewFuzzyEngine()
	if err != nil {
		return nil, err
	}
	defer engine.Close()

	if _, err := engine.RebuildIndex(vocabulary, nil); err != nil {
		return nil, err
	}

	result := make(map[string][]ats.WordMatch, len(queryWords))
	for _, qw := range queryWords {
		matches, _, err := engine.FindMatches(qw, fuzzyax.VocabPredicates, limit, minScore)
		if err != nil {
			continue
		}
		for _, match := range matches {
			result[qw] = append(result[qw], ats.WordMatch{
				Value: match.Value,
				Score: match.Score,
			})
		}
	}
	return result, nil
}

// Ensure FuzzyRichSearchMatcher implements ats.RichSearchMatcher
var _ ats.RichSearchMatcher = (*FuzzyRichSearchMatcher)(nil)
