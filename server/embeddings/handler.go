// Package embeddings provides HTTP handlers for the QNTX embedding service:
// semantic search, vector generation, HDBSCAN clustering, UMAP projection.
//
// All handler files are gated behind the `cgo && rustembeddings` build tag.
// When the tag is absent, the stub file provides no-op handlers that return
// 501 Not Implemented.
package embeddings

import (
	"context"
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"go.uber.org/zap"
)

// Service defines the embedding operations used by handlers.
// Matches the interface already defined on QNTXServer.embeddingService.
type Service interface {
	GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error)
	GenerateBatchEmbeddings(texts []string) (*embeddings.BatchEmbeddingResult, error)
	GetModelInfo() (*embeddings.ModelInfo, error)
	SerializeEmbedding(embedding []float32) ([]byte, error)
	DeserializeEmbedding(data []byte) ([]float32, error)
	ComputeSimilarity(a, b []float32) (float32, error)
	Close() error
}

// ReduceFunc calls the reduce plugin via gRPC for projection operations.
type ReduceFunc func(ctx context.Context, method, path string, body []byte) ([]byte, error)

// Handler handles HTTP requests for the embedding service.
type Handler struct {
	DB          *sql.DB
	Store       *storage.EmbeddingStore
	Service     Service
	ATSStore    ats.AttestationStore
	Logger      *zap.SugaredLogger
	CallReduce  ReduceFunc // optional: for projection via reduce plugin
}

// getAttestationByID retrieves a single attestation through the attestation store (Rust FFI).
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
