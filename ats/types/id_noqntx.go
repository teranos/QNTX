//go:build !qntxwasm

package types

import (
	id "github.com/teranos/vanity-id"
)

// generateASUID falls back to vanity-id when WASM is not available.
func generateASUID(_, subject, predicate, context string) (string, error) {
	return id.GenerateASID(subject, predicate, context, "")
}
