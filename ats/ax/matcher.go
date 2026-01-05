package ax

// Matcher defines the interface for fuzzy matching implementations.
// Both the built-in Go implementation and CGO-backed Rust implementation
// satisfy this interface.
type Matcher interface {
	// FindMatches finds predicates that match the query using fuzzy logic.
	// Returns matching predicates from the provided vocabulary.
	FindMatches(queryPredicate string, allPredicates []string) []string

	// FindContextMatches finds contexts that match the query using fuzzy logic.
	// Returns matching contexts from the provided vocabulary.
	FindContextMatches(queryContext string, allContexts []string) []string
}

// Ensure FuzzyMatcher implements Matcher
var _ Matcher = (*FuzzyMatcher)(nil)
