// Package embeddings provides HTTP handlers for the QNTX embedding service:
// semantic search, vector generation, HDBSCAN clustering, UMAP projection.
//
// Embedding inference and clustering are served by an external plugin via gRPC.
package embeddings

import (
	"context"
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"go.uber.org/zap"
)

// Service defines the embedding operations used by handlers.
// Matches the interface already defined on QNTXServer.embeddingService.
type Service interface {
	GenerateEmbedding(text, model string) (*EmbeddingResult, error)
	GenerateBatchEmbeddings(texts []string, model string) (*BatchEmbeddingResult, error)
	GetModelInfo(model string) (*ModelInfo, error)
	SerializeEmbedding(embedding []float32) ([]byte, error)
	DeserializeEmbedding(data []byte) ([]float32, error)
	ComputeSimilarity(a, b []float32) (float32, error)
	Close() error
}

// EmbeddingServiceForClustering is the subset of the embedding service needed for clustering and projection.
type EmbeddingServiceForClustering interface {
	DeserializeEmbedding(data []byte) ([]float32, error)
	SerializeEmbedding(embedding []float32) ([]byte, error)
}

// ClusterFunc runs HDBSCAN clustering on a flat array of embeddings.
// Plugin path passes PluginEmbeddingService.ClusterHDBSCAN.
type ClusterFunc func(data []float32, nPoints, dims, minClusterSize int) (*ClusterResult, error)

// ReduceFunc calls the reduce plugin via gRPC for projection operations.
type ReduceFunc func(ctx context.Context, method, path string, body []byte) ([]byte, error)

// GroundWriteFunc writes an attestation to Ground's standalone database.
type GroundWriteFunc func(dbPath string, as *types.As, logger *zap.SugaredLogger)

// Handler handles HTTP requests for the embedding service.
type Handler struct {
	DB           *sql.DB
	Store        *storage.EmbeddingStore
	Service      Service
	ATSStore     ats.AttestationStore
	Logger       *zap.SugaredLogger
	CallReduce   ReduceFunc      // optional: for projection via reduce plugin
	ClusterFunc  ClusterFunc     // required: clustering via plugin gRPC
	Invalidator  func()          // cluster cache invalidation callback
	GroundDBPath string          // for cluster lifecycle attestations
	GroundWrite  GroundWriteFunc // writes deferred news to Ground's DB
}

// getAttestationByID retrieves a single attestation through the attestation store.
// Falls back to Go's *sql.DB if the store doesn't support direct get.
func (h *Handler) getAttestationByID(id string) (*types.As, error) {
	type singleGetter interface {
		GetAttestation(id string) (*types.As, error)
	}
	if sg, ok := h.ATSStore.(singleGetter); ok {
		return sg.GetAttestation(id)
	}
	return storage.GetAttestationByID(h.DB, id)
}
