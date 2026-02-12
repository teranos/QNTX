package ax

// MatcherBackend indicates which fuzzy matching implementation is in use
type MatcherBackend string

const (
	// MatcherBackendGo indicates no WASM backend is available
	MatcherBackendGo MatcherBackend = "go"
	// MatcherBackendWasm indicates the WASM-backed Rust implementation (via wazero)
	MatcherBackendWasm MatcherBackend = "wasm"
)

// Matcher defines the interface for fuzzy matching implementations.
type Matcher interface {
	// FindMatches finds predicates that match the query using fuzzy logic.
	// Returns matching predicates from the provided vocabulary.
	FindMatches(queryPredicate string, allPredicates []string) []string

	// FindContextMatches finds contexts that match the query using fuzzy logic.
	// Returns matching contexts from the provided vocabulary.
	FindContextMatches(queryContext string, allContexts []string) []string

	// Backend returns which implementation is being used (go or wasm)
	Backend() MatcherBackend

	// SetLogger sets an optional logger for debug output.
	// Implementations may ignore this if logging is not supported.
	SetLogger(logger interface{})
}

// hashStrings computes a simple FNV-1a hash of a string slice for change detection.
func hashStrings(strs []string) uint64 {
	var hash uint64 = 14695981039346656037 // FNV-1a offset basis
	for _, s := range strs {
		for i := 0; i < len(s); i++ {
			hash ^= uint64(s[i])
			hash *= 1099511628211 // FNV-1a prime
		}
		hash ^= uint64(0xff) // separator
		hash *= 1099511628211
	}
	return hash
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

// Ensure WasmMatcher implements Matcher
var _ Matcher = (*WasmMatcher)(nil)
