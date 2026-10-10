package services

import (
	"context"
	"sync"

	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// VectorSearchServer implements the VectorSearchService gRPC server.
// Starts with no backend — SetService must be called once the provider
// plugin initializes (same lazy pattern as EmbeddingServer).
type VectorSearchServer struct {
	protocol.UnimplementedVectorSearchServiceServer
	mu         sync.RWMutex
	service    protocol.VectorSearchServiceClient
	serviceErr error // the backend not registered yet
	authToken  string
	logger     *zap.SugaredLogger
}

// NewVectorSearchServer creates a new vector search gRPC server.
func NewVectorSearchServer(authToken string, logger *zap.SugaredLogger) *VectorSearchServer {
	return &VectorSearchServer{
		serviceErr: errors.New("vector search service not initialized"),
		authToken:  authToken,
		logger:     logger,
	}
}

// SetService registers the vector search backend after provider initialization.
func (s *VectorSearchServer) SetService(client protocol.VectorSearchServiceClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.service, s.serviceErr = client, nil
	s.logger.Infow("VectorSearch gRPC service backend registered")
}

// HasProvider returns true if a vector search backend is registered.
func (s *VectorSearchServer) HasProvider() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serviceErr == nil
}

// backend is the registered vector search backend, or why there is none yet.
func (s *VectorSearchServer) backend() (protocol.VectorSearchServiceClient, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.service, s.serviceErr
}

// Search finds the nearest neighbors to a query vector in a named index.
func (s *VectorSearchServer) Search(ctx context.Context, req *protocol.VectorSearchRequest) (*protocol.VectorSearchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	svc, err := s.backend()
	if err != nil {
		return nil, err
	}

	index, err := named.TextOf("index", req.Index)
	if err != nil {
		return nil, err
	}
	query, err := named.ItemsOf("query vector", req.QueryVector)
	if err != nil {
		return nil, err
	}

	return svc.Search(ctx, &protocol.VectorSearchRequest{
		AuthToken:   req.AuthToken,
		Index:       index.String(),
		QueryVector: query.All(),
		TopK:        req.TopK,
	})
}

// AddVectors inserts vectors into a named index.
func (s *VectorSearchServer) AddVectors(ctx context.Context, req *protocol.AddVectorsRequest) (*protocol.AddVectorsResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	svc, err := s.backend()
	if err != nil {
		return nil, err
	}

	index, err := named.TextOf("index", req.Index)
	if err != nil {
		return nil, err
	}
	vectors, err := named.ItemsOf("vectors", req.Vectors)
	if err != nil {
		return nil, err
	}

	return svc.AddVectors(ctx, &protocol.AddVectorsRequest{
		AuthToken: req.AuthToken,
		Index:     index.String(),
		Vectors:   vectors.All(),
	})
}

// CreateIndex creates a new named vector index.
func (s *VectorSearchServer) CreateIndex(ctx context.Context, req *protocol.CreateIndexRequest) (*protocol.CreateIndexResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	svc, err := s.backend()
	if err != nil {
		return nil, err
	}

	name, err := named.TextOf("index name", req.Name)
	if err != nil {
		return nil, err
	}
	dimensions, err := named.CountOf("dimensions", int(req.Dimensions))
	if err != nil {
		return nil, err
	}

	return svc.CreateIndex(ctx, &protocol.CreateIndexRequest{
		AuthToken:  req.AuthToken,
		Name:       name.String(),
		Dimensions: int32(dimensions.Int()),
	})
}
