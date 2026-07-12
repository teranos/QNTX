package services

import (
	"context"
	"sync"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// EmbeddingServer implements the EmbeddingService gRPC server.
// Starts with no backend — SetService must be called once the embedding
// engine is initialized (same lazy pattern as LLMServer).
type EmbeddingServer struct {
	protocol.UnimplementedEmbeddingServiceServer
	mu        sync.RWMutex
	service   embeddingBackend
	store     *storage.EmbeddingStore
	authToken string
	logger    *zap.SugaredLogger
}

// embeddingBackend is the subset of ManagedEmbeddingService needed by the gRPC server.
type embeddingBackend interface {
	GenerateEmbedding(text, model string) (*serverembeddings.EmbeddingResult, error)
	GenerateBatchEmbeddings(texts []string, model string) (*serverembeddings.BatchEmbeddingResult, error)
	GetModelInfo(model string) (*serverembeddings.ModelInfo, error)
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

// SetStore registers the embedding store for cluster query RPCs.
func (s *EmbeddingServer) SetStore(store *storage.EmbeddingStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
	s.logger.Infow("Embedding gRPC store registered")
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
		return nil, errors.New("embedding service not initialized")
	}

	if req.Text == "" {
		return nil, errors.New("text cannot be empty")
	}

	result, err := svc.GenerateEmbedding(req.Text, req.Model)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to generate embedding")
	}

	info, err := svc.GetModelInfo(req.Model)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get model info")
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
		return nil, errors.New("embedding service not initialized")
	}

	if len(req.Texts) == 0 {
		return nil, errors.New("no texts provided")
	}

	result, err := svc.GenerateBatchEmbeddings(req.Texts, req.Model)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to generate batch embeddings")
	}

	info, err := svc.GetModelInfo(req.Model)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get model info")
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

// GetLabelEligibleClusters returns clusters eligible for labeling.
func (s *EmbeddingServer) GetLabelEligibleClusters(ctx context.Context, req *protocol.GetLabelEligibleClustersRequest) (*protocol.GetLabelEligibleClustersResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()

	if store == nil {
		return nil, errors.New("embedding store not initialized")
	}

	eligible, err := store.GetLabelEligibleClusters(int(req.MinSize), int(req.CooldownDays), int(req.Limit))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query eligible clusters")
	}

	clusters := make([]*protocol.EligibleCluster, len(eligible))
	for i, c := range eligible {
		clusters[i] = &protocol.EligibleCluster{
			Id:      int32(c.ID),
			Members: int32(c.Members),
		}
	}

	return &protocol.GetLabelEligibleClustersResponse{Clusters: clusters}, nil
}

// SampleClusterTexts returns random sample texts from a cluster.
func (s *EmbeddingServer) SampleClusterTexts(ctx context.Context, req *protocol.SampleClusterTextsRequest) (*protocol.SampleClusterTextsResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()

	if store == nil {
		return nil, errors.New("embedding store not initialized")
	}

	texts, err := store.SampleClusterTexts(int(req.ClusterId), int(req.SampleSize))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to sample texts for cluster %d", req.ClusterId)
	}

	return &protocol.SampleClusterTextsResponse{Texts: texts}, nil
}

// SetClusterLabel sets or updates the label on a cluster.
func (s *EmbeddingServer) SetClusterLabel(ctx context.Context, req *protocol.SetClusterLabelRequest) (*protocol.SetClusterLabelResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()

	if store == nil {
		return nil, errors.New("embedding store not initialized")
	}

	if req.Label == "" {
		return nil, errors.New("label cannot be empty")
	}

	if err := store.UpdateClusterLabel(int(req.ClusterId), req.Label); err != nil {
		return nil, errors.Wrapf(err, "failed to set label for cluster %d", req.ClusterId)
	}

	return &protocol.SetClusterLabelResponse{}, nil
}
