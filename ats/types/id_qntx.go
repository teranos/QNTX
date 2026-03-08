//go:build qntxwasm

package types

import (
	"github.com/teranos/QNTX/ats/wasm"
	"github.com/teranos/QNTX/errors"
)

// generateASUID generates an ASUID via the Rust WASM engine.
func generateASUID(prefix, subject, predicate, context string) (string, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return "", errors.Wrapf(err, "failed to get WASM engine for %s %s", predicate, subject)
	}
	return engine.GenerateASUID(prefix, subject, predicate, context)
}
