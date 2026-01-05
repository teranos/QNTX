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

// Ensure FuzzyMatcher implements Matcher
var _ Matcher = (*FuzzyMatcher)(nil)
