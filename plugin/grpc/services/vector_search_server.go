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
	mu        sync.RWMutex
	backing   vectorBacking
	authToken string
	logger    *zap.SugaredLogger
}

// vectorBacking is what serves vector search: a registered backend, or none
// yet, and why.
type vectorBacking interface {
	backsVectorSearch()
}

// vectorBackend is the registered vector search backend.
type vectorBackend struct {
	client protocol.VectorSearchServiceClient
}

// noVectorBackend is no backend registered yet, and why.
type noVectorBackend struct {
	why error
}

func (vectorBackend) backsVectorSearch()   {}
func (noVectorBackend) backsVectorSearch() {}

// NewVectorSearchServer creates a new vector search gRPC server.
func NewVectorSearchServer(authToken string, logger *zap.SugaredLogger) *VectorSearchServer {
	return &VectorSearchServer{
		backing:   noVectorBackend{why: errors.New("vector search service not initialized")},
		authToken: authToken,
		logger:    logger,
	}
}

// SetService registers the vector search backend after provider initialization.
func (s *VectorSearchServer) SetService(client protocol.VectorSearchServiceClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backing = vectorBackend{client: client}
	s.logger.Infow("VectorSearch gRPC service backend registered")
}

// HasProvider returns true if a vector search backend is registered.
func (s *VectorSearchServer) HasProvider() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch s.backing.(type) {
	case vectorBackend:
		return true
	case noVectorBackend:
	}
	return false
}

// backend is the registered vector search backend, or why there is none yet.
func (s *VectorSearchServer) backend() (protocol.VectorSearchServiceClient, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch backing := s.backing.(type) {
	case vectorBackend:
		return backing.client, nil
	case noVectorBackend:
		return nil, backing.why
	}
	return nil, errors.Newf("vector search names neither a backend nor why it has none: %T", s.backing)
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
