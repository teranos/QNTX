package grpc

import (
	"context"
	"sync"

	"github.com/teranos/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// VectorSearchServer implements the VectorSearchService gRPC server.
// Starts with no backend — SetService must be called once the provider
// plugin initializes (same lazy pattern as EmbeddingServer).
type VectorSearchServer struct {
	protocol.UnimplementedVectorSearchServiceServer
	mu        sync.RWMutex
	service   protocol.VectorSearchServiceClient
	authToken string
	logger    *zap.SugaredLogger
}

// NewVectorSearchServer creates a new vector search gRPC server.
func NewVectorSearchServer(authToken string, logger *zap.SugaredLogger) *VectorSearchServer {
	return &VectorSearchServer{
		authToken: authToken,
		logger:    logger,
	}
}

// SetService registers the vector search backend after provider initialization.
func (s *VectorSearchServer) SetService(client protocol.VectorSearchServiceClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = client
	s.logger.Infow("VectorSearch gRPC service backend registered")
}

// HasProvider returns true if a vector search backend is registered.
func (s *VectorSearchServer) HasProvider() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.service != nil
}

// Search finds the nearest neighbors to a query vector in a named index.
func (s *VectorSearchServer) Search(ctx context.Context, req *protocol.VectorSearchRequest) (*protocol.VectorSearchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	svc := s.service
	s.mu.RUnlock()

	if svc == nil {
		return nil, errors.New("vector search service not initialized")
	}

	if req.Index == "" {
		return nil, errors.New("index name cannot be empty")
	}
	if len(req.QueryVector) == 0 {
		return nil, errors.New("query vector cannot be empty")
	}

	return svc.Search(ctx, req)
}

// AddVectors inserts vectors into a named index.
func (s *VectorSearchServer) AddVectors(ctx context.Context, req *protocol.AddVectorsRequest) (*protocol.AddVectorsResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	svc := s.service
	s.mu.RUnlock()

	if svc == nil {
		return nil, errors.New("vector search service not initialized")
	}

	if req.Index == "" {
		return nil, errors.New("index name cannot be empty")
	}
	if len(req.Vectors) == 0 {
		return nil, errors.New("no vectors provided")
	}

	return svc.AddVectors(ctx, req)
}

// CreateIndex creates a new named vector index.
func (s *VectorSearchServer) CreateIndex(ctx context.Context, req *protocol.CreateIndexRequest) (*protocol.CreateIndexResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	s.mu.RLock()
	svc := s.service
	s.mu.RUnlock()

	if svc == nil {
		return nil, errors.New("vector search service not initialized")
	}

	if req.Name == "" {
		return nil, errors.New("index name cannot be empty")
	}
	if req.Dimensions <= 0 {
		return nil, errors.Newf("dimensions must be positive, got %d", req.Dimensions)
	}

	return svc.CreateIndex(ctx, req)
}
