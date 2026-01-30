package ax

// MatcherBackend indicates which fuzzy matching implementation is in use
type MatcherBackend string

const (
	// MatcherBackendRust indicates the CGO-backed Rust implementation (qntx-core)
	MatcherBackendRust MatcherBackend = "rust"
)

// Matcher defines the interface for fuzzy matching implementations.
type Matcher interface {
	// FindMatches finds predicates that match the query using fuzzy logic.
	// Returns matching predicates from the provided vocabulary.
	FindMatches(queryPredicate string, allPredicates []string) []string

	// FindContextMatches finds contexts that match the query using fuzzy logic.
	// Returns matching contexts from the provided vocabulary.
	FindContextMatches(queryContext string, allContexts []string) []string

	// Backend returns which implementation is being used
	Backend() MatcherBackend

	// SetLogger sets an optional logger for debug output.
	SetLogger(logger interface{})
}

// NewDefaultMatcher creates the Rust-backed matcher (qntx-core via CGO).
// Panics if the Rust library is not available - build with -tags rustfuzzy.
func NewDefaultMatcher() Matcher {
	matcher, err := NewCGOMatcher()
	if err != nil {
		panic("qntx-core fuzzy matcher not available: " + err.Error() + " (build with -tags rustfuzzy)")
	}
	return matcher
}

// DetectBackend returns the fuzzy backend (always Rust with qntx-core).
func DetectBackend() MatcherBackend {
	return MatcherBackendRust
}
