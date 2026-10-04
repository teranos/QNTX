package embeddings

import (
	"time"

	"github.com/teranos/QNTX/plugin/embedding"
)

// ClusterNoise is the label assigned to points not belonging to any cluster.
// HDBSCAN convention: -1 means noise/outlier.
const ClusterNoise = -1

// The results the embedding service answers with live in plugin/embedding,
// which plugins import without importing the server.
type (
	ModelInfo            = embedding.ModelInfo
	EmbeddingResult      = embedding.EmbeddingResult
	BatchEmbeddingResult = embedding.BatchEmbeddingResult
)

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
	ID         string         `json:"id"`
	Text       string         `json:"text"`
	Distance   float32        `json:"distance"`
	Similarity float32        `json:"similarity"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}
