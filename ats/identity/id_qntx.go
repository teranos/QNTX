//go:build qntxwasm

package identity

import (
	"github.com/teranos/QNTX/ats/wasm"
	"github.com/teranos/errors"
)

// generateASUID generates an ASUID via the Rust WASM engine.
func generateASUID(prefix, subject, predicate, context string) (string, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return "", errors.Wrapf(err, "WASM engine unavailable for ASUID %s/%s/%s", subject, predicate, context)
	}
	return engine.GenerateASUID(prefix, subject, predicate, context)
}

// generateCompactASUID generates a compact ASUID via the Rust WASM engine.
func generateCompactASUID(prefix, name string) (string, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return "", errors.Wrapf(err, "WASM engine unavailable for compact ASUID %s/%s", prefix, name)
	}
	return engine.GenerateCompactASUID(prefix, name)
}

// generateRandomID generates a random ID via the Rust WASM engine.
func generateRandomID(length int) (string, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return "", errors.Wrapf(err, "WASM engine unavailable for random ID (length %d)", length)
	}
	return engine.GenerateRandomID(length)
}
