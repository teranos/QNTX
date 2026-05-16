//go:build !qntxwasm

package ax

// NewDefaultClassifier panics without qntxwasm — WASM is required for classification.
func NewDefaultClassifier(config TemporalConfig) Classifier {
	panic("WASM classifier required: built without qntxwasm tag — run `make wasm`")
}
