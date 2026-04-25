package embeddings

import "time"

// ClusterNoise is the label assigned to points not belonging to any cluster.
// HDBSCAN convention: -1 means noise/outlier.
const ClusterNoise = -1

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

// ClusterResult holds the output of HDBSCAN clustering.
type ClusterResult struct {
	Labels        []int32     `json:"labels"`
	Probabilities []float32   `json:"probabilities"`
	NClusters     int         `json:"n_clusters"`
	NPoints       int         `json:"n_points"`
	NNoise        int         `json:"n_noise"`
	Centroids     [][]float32 `json:"centroids"` // one centroid per cluster, indexed by label
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
