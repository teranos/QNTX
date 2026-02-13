package embeddings

import (
	"time"
)

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

// EmbeddingService defines the interface for embedding operations
type EmbeddingService interface {
	// Initialize the engine with a model
	Init(modelPath string) error

	// Get model information
	ModelInfo() (*ModelInfo, error)

	// Embed a single text
	Embed(text string) (*EmbeddingResult, error)

	// Embed multiple texts
	EmbedBatch(texts []string) (*BatchEmbeddingResult, error)

	// Clean up resources
	Close() error
}

// VectorSearchResult represents a semantic search result
type VectorSearchResult struct {
	ID         string                 `json:"id"`
	Text       string                 `json:"text"`
	Distance   float32                `json:"distance"`
	Similarity float32                `json:"similarity"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}
