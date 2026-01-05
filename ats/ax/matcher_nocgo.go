//go:build !cgo || !rustfuzzy

package ax

import "errors"

// CGOMatcher is a stub when the rustfuzzy build tag is not set.
// To enable CGO fuzzy matching, build with: go build -tags rustfuzzy
type CGOMatcher struct{}

// NewCGOMatcher returns an error when CGO is disabled.
func NewCGOMatcher() (*CGOMatcher, error) {
	return nil, errors.New("CGO fuzzy matcher not available: built without CGO support")
}

// Close is a no-op for the stub.
func (m *CGOMatcher) Close() error {
	return nil
}

// FindMatches is not available without CGO.
func (m *CGOMatcher) FindMatches(queryPredicate string, allPredicates []string) []string {
	return nil
}

// FindContextMatches is not available without CGO.
func (m *CGOMatcher) FindContextMatches(queryContext string, allContexts []string) []string {
	return nil
}
