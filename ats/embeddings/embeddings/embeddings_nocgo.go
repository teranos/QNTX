//go:build !cgo || !rustembeddings

package embeddings

import (
	"github.com/teranos/QNTX/errors"
)

// StubEmbeddingService is a no-op implementation when CGO is not available
type StubEmbeddingService struct{}

// NewEmbeddingService creates a stub embedding service
func NewEmbeddingService() EmbeddingService {
	return &StubEmbeddingService{}
}

// Init is a no-op
func (s *StubEmbeddingService) Init(modelPath string) error {
	return errors.New("embeddings not available: built without rustembeddings support")
}

// ModelInfo returns an error
func (s *StubEmbeddingService) ModelInfo() (*ModelInfo, error) {
	return nil, errors.New("embeddings not available: built without rustembeddings support")
}

// Embed returns an error
func (s *StubEmbeddingService) Embed(text string) (*EmbeddingResult, error) {
	return nil, errors.New("embeddings not available: built without rustembeddings support")
}

// EmbedBatch returns an error
func (s *StubEmbeddingService) EmbedBatch(texts []string) (*BatchEmbeddingResult, error) {
	return nil, errors.New("embeddings not available: built without rustembeddings support")
}

// Close is a no-op
func (s *StubEmbeddingService) Close() error {
	return nil
}
