// Package embedding holds the results an embedding service answers with,
// shared by the server and plugins without either importing the other.
package embedding

// ModelInfo contains information about the loaded embedding model
type ModelInfo struct {
	Name              string `json:"name"`
	Dimensions        int    `json:"dimensions"`
	MaxSequenceLength int    `json:"max_sequence_length"`
}

// EmbeddingResult represents the result of embedding a single text
type EmbeddingResult struct {
	Text        string    `json:"text"`
	Embedding   []float32 `json:"embedding"`
	Tokens      int       `json:"tokens"`
	InferenceMS float64   `json:"inference_ms"`
}

// BatchEmbeddingResult represents the result of embedding multiple texts
type BatchEmbeddingResult struct {
	Embeddings       []EmbeddingResult `json:"embeddings"`
	TotalTokens      int               `json:"total_tokens"`
	TotalInferenceMS float64           `json:"total_inference_ms"`
}
