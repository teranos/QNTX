//go:build !qntxwasm

package identity

import (
	id "github.com/teranos/vanity-id"
)

// generateASUID falls back to vanity-id when WASM is not available.
// TODO(#645): remove this file once vanity-id is retired.
func generateASUID(prefix, subject, predicate, context string) (string, error) {
	if prefix != "AS" {
		return id.GenerateASIDWithPrefix(prefix, subject, predicate, context, "")
	}
	return id.GenerateASID(subject, predicate, context, "")
}
