package grpc

import (
	"context"
	"fmt"
	"sync"

	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// EmbeddingServer implements the EmbeddingService gRPC server.
// Starts with no backend — SetService must be called once the embedding
// engine is initialized (same lazy pattern as LLMServer).
type EmbeddingServer struct {
	protocol.UnimplementedEmbeddingServiceServer
	mu        sync.RWMutex
	service   embeddingBackend
	authToken string
	logger    *zap.SugaredLogger
}

// embeddingBackend is the subset of ManagedEmbeddingService needed by the gRPC server.
type embeddingBackend interface {
	GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error)
	GenerateBatchEmbeddings(texts []string) (*embeddings.BatchEmbeddingResult, error)
	GetModelInfo() (*embeddings.ModelInfo, error)
}

// NewEmbeddingServer creates a new embedding gRPC server.
func NewEmbeddingServer(authToken string, logger *zap.SugaredLogger) *EmbeddingServer {
	return &EmbeddingServer{
		authToken: authToken,
		logger:    logger,
	}
}

// SetService registers the embedding backend after initialization.
func (s *EmbeddingServer) SetService(svc embeddingBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = svc
	s.logger.Infow("Embedding gRPC service backend registered")
}

// Embed generates a vector embedding for a single text.
func (s *EmbeddingServer) Embed(ctx context.Context, req *protocol.EmbedRequest) (*protocol.EmbedResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	svc := s.service
	s.mu.RUnlock()

	if svc == nil {
		return nil, fmt.Errorf("embedding service not initialized")
	}

	if req.Text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	result, err := svc.GenerateEmbedding(req.Text)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	info, err := svc.GetModelInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get model info: %w", err)
	}

	return &protocol.EmbedResponse{
		Vector:     result.Embedding,
		Dimensions: int32(info.Dimensions),
		Model:      info.Name,
		Tokens:     int32(result.Tokens),
	}, nil
}

// BatchEmbed generates vector embeddings for multiple texts.
func (s *EmbeddingServer) BatchEmbed(ctx context.Context, req *protocol.BatchEmbedRequest) (*protocol.BatchEmbedResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	svc := s.service
	s.mu.RUnlock()

	if svc == nil {
		return nil, fmt.Errorf("embedding service not initialized")
	}

	if len(req.Texts) == 0 {
		return nil, fmt.Errorf("no texts provided")
	}

	result, err := svc.GenerateBatchEmbeddings(req.Texts)
	if err != nil {
		return nil, fmt.Errorf("failed to generate batch embeddings: %w", err)
	}

	info, err := svc.GetModelInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get model info: %w", err)
	}

	vectors := make([]*protocol.EmbeddingVector, len(result.Embeddings))
	for i, emb := range result.Embeddings {
		vectors[i] = &protocol.EmbeddingVector{
			Vector: emb.Embedding,
			Tokens: int32(emb.Tokens),
		}
	}

	return &protocol.BatchEmbedResponse{
		Results:     vectors,
		Dimensions:  int32(info.Dimensions),
		Model:       info.Name,
		TotalTokens: int32(result.TotalTokens),
	}, nil
}
