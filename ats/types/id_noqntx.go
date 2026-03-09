//go:build !qntxwasm

package types

import (
	id "github.com/teranos/vanity-id"
)

// generateASUID falls back to vanity-id when WASM is not available.
// TODO(#645): remove this file once all callers use qntxwasm and vanity-id is retired.
func generateASUID(_, subject, predicate, context string) (string, error) {
	return id.GenerateASID(subject, predicate, context, "")
}
