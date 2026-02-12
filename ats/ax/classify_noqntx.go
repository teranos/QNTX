//go:build !qntxwasm

package ax

import (
	"github.com/teranos/QNTX/ats/ax/classification"
)

// NewDefaultClassifier panics without qntxwasm — WASM is required for classification.
func NewDefaultClassifier(config classification.TemporalConfig) Classifier {
	panic("WASM classifier required: built without qntxwasm tag — run `make wasm`")
}
