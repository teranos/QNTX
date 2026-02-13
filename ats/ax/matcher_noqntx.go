//go:build !qntxwasm

package ax

import "github.com/teranos/QNTX/errors"

// WasmMatcher is a stub when the qntxwasm build tag is not set.
type WasmMatcher struct{}

// NewWasmMatcher returns an error when WASM is not available.
func NewWasmMatcher() (*WasmMatcher, error) {
	return nil, errors.New("WASM fuzzy matcher not available: built without qntxwasm tag — run `make wasm`")
}

// NewDefaultMatcher panics without qntxwasm — WASM is required for fuzzy matching.
func NewDefaultMatcher() Matcher {
	panic("WASM fuzzy matcher required: built without qntxwasm tag — run `make wasm`")
}

// DetectBackend returns Go (non-WASM) when built without qntxwasm tag.
func DetectBackend() MatcherBackend {
	return MatcherBackendGo
}

// FindMatches is not available without WASM.
func (m *WasmMatcher) FindMatches(queryPredicate string, allPredicates []string) []string {
	return nil
}

// FindContextMatches is not available without WASM.
func (m *WasmMatcher) FindContextMatches(queryContext string, allContexts []string) []string {
	return nil
}

// Backend returns Go since WASM is not available.
func (m *WasmMatcher) Backend() MatcherBackend {
	return MatcherBackendGo
}

// SetLogger is a no-op for the stub.
func (m *WasmMatcher) SetLogger(logger interface{}) {}
