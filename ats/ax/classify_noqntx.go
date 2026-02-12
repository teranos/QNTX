//go:build !qntxwasm

package ax

import (
	"github.com/teranos/QNTX/ats/ax/classification"
)

// NewDefaultClassifier without qntxwasm returns the Go classifier.
// NOTE: This fallback exists only for builds without WASM. Once Rust WASM
// becomes the standard build path, this file and the Go classification
// package can be deleted — the Rust engine is the single source of truth.
func NewDefaultClassifier(config classification.TemporalConfig) Classifier {
	return NewGoClassifier(config)
}
