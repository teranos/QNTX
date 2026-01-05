package ax

// MatcherBackend indicates which fuzzy matching implementation is in use
type MatcherBackend string

const (
	// MatcherBackendGo indicates the built-in Go implementation
	MatcherBackendGo MatcherBackend = "go"
	// MatcherBackendRust indicates the CGO-backed Rust implementation
	MatcherBackendRust MatcherBackend = "rust"
)

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

	// Backend returns which implementation is being used (go or rust)
	Backend() MatcherBackend
}

// NewDefaultMatcher creates the best available matcher implementation.
// Prefers Rust CGO matcher if available (built with -tags rustfuzzy),
// otherwise falls back to Go implementation.
func NewDefaultMatcher() Matcher {
	// Try CGO matcher first (only available with -tags rustfuzzy)
	if matcher, err := NewCGOMatcher(); err == nil {
		return matcher
	}
	// Fall back to Go implementation
	return NewFuzzyMatcher()
}

// Ensure FuzzyMatcher implements Matcher
var _ Matcher = (*FuzzyMatcher)(nil)
