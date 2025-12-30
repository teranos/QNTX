package ax

import (
	"strings"

	id "github.com/teranos/vanity-id"
)

// NOTE: This fuzzy matching system is an incremental improvement over the previous
// predicate-only matching. It provides basic context matching capabilities and
// simple organization name handling. A more sophisticated matching system with
// semantic understanding, learning capabilities, and advanced NLP features is
// planned for future development. This current implementation focuses on
// "good enough" improvements rather than a perfect solution.
//
// See GitHub issue #32 for planned advanced fuzzy matching system improvements.

// FuzzyMatcher provides fuzzy matching for predicates and contexts
// This is an incremental improvement - see package note about future plans
type FuzzyMatcher struct {
	// Simplified version without caching initially
}

// NewFuzzyMatcher creates a new fuzzy matcher
func NewFuzzyMatcher() *FuzzyMatcher {
	return &FuzzyMatcher{}
}

// NewPredicateMatcher creates a new predicate matcher (backward compatibility)
func NewPredicateMatcher() *FuzzyMatcher {
	return NewFuzzyMatcher()
}

// FindMatches finds predicates that match the query using fuzzy logic
func (fm *FuzzyMatcher) FindMatches(queryPredicate string, allPredicates []string) []string {
	if strings.TrimSpace(queryPredicate) == "" {
		return []string{}
	}

	matches := []string{}
	queryLower := strings.ToLower(strings.TrimSpace(queryPredicate))

	for _, predicate := range allPredicates {
		if fm.isMatch(queryLower, predicate) {
			matches = append(matches, predicate)
		}
	}

	return matches
}

// FindContextMatches finds contexts that match the query using fuzzy logic
func (fm *FuzzyMatcher) FindContextMatches(queryContext string, allContexts []string) []string {
	if strings.TrimSpace(queryContext) == "" {
		return []string{}
	}

	matches := []string{}
	queryLower := strings.ToLower(strings.TrimSpace(queryContext))

	for _, context := range allContexts {
		if fm.isContextMatch(queryLower, context) {
			matches = append(matches, context)
		}
	}

	return matches
}

// isMatch determines if a value matches the query
// Uses NormalizeForLookup for ID-like values to handle 0/O and 1/I confusion
func (fm *FuzzyMatcher) isMatch(query, value string) bool {
	valueLower := strings.ToLower(strings.TrimSpace(value))

	// 1. Exact match
	if query == valueLower {
		return true
	}

	// 2. Substring match - query appears anywhere in value
	if strings.Contains(valueLower, query) {
		return true
	}

	// 3. Word boundary match - query matches a complete word in value
	words := strings.Fields(valueLower)
	for _, word := range words {
		if word == query {
			return true
		}
	}

	// 4. Normalized ID match - handles 0/O and 1/I confusion for vanity IDs
	// Only apply if query looks like an ID (uppercase alphanumeric)
	if isLikelyID(query) {
		normalizedQuery := id.NormalizeForLookup(query)
		normalizedValue := id.NormalizeForLookup(value)
		if normalizedQuery == normalizedValue {
			return true
		}
		if strings.Contains(normalizedValue, normalizedQuery) {
			return true
		}
	}

	return false
}

// isLikelyID returns true if the string looks like a vanity ID (short, alphanumeric)
func isLikelyID(s string) bool {
	if len(s) < 2 || len(s) > 12 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// isContextMatch determines if a context value matches the query with enhanced fuzzy logic
// NOTE: This is a basic implementation - more sophisticated matching planned for future
// Uses NormalizeForLookup for ID-like values to handle 0/O and 1/I confusion
func (fm *FuzzyMatcher) isContextMatch(query, value string) bool {
	valueLower := strings.ToLower(strings.TrimSpace(value))

	// 1. Exact match
	if query == valueLower {
		return true
	}

	// 2. Substring match - query appears anywhere in value
	if strings.Contains(valueLower, query) {
		return true
	}

	// 3. Word boundary match - query matches a complete word in value
	words := strings.Fields(valueLower)
	for _, word := range words {
		if word == query {
			return true
		}
	}

	// 4. Normalized ID match - handles 0/O and 1/I confusion for vanity IDs
	if isLikelyID(query) {
		normalizedQuery := id.NormalizeForLookup(query)
		normalizedValue := id.NormalizeForLookup(value)
		if normalizedQuery == normalizedValue {
			return true
		}
		if strings.Contains(normalizedValue, normalizedQuery) {
			return true
		}
	}

	return false
}

// removeDuplicates removes duplicate strings from a slice
func removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}
