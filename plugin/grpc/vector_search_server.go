package grpc

import (
	"context"
	"fmt"
	"sync"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// VectorSearchServer is the core-side gRPC server that routes vector search
// requests to the provider plugin (e.g. qntx-faiss). It implements
// protocol.VectorSearchServiceServer and holds a VectorSearchServiceClient
// connection to the provider plugin.
//
// Starts empty — the provider registers after init, same pattern as
// LLMServer and SearchServer.
type VectorSearchServer struct {
	protocol.UnimplementedVectorSearchServiceServer
	mu        sync.RWMutex
	provider  protocol.VectorSearchServiceClient
	name      string // provider name for logging
	authToken string
	logger    *zap.SugaredLogger
}

// NewVectorSearchServer creates a new vector search routing server.
// Starts empty — provider registers after init.
func NewVectorSearchServer(authToken string, logger *zap.SugaredLogger) *VectorSearchServer {
	return &VectorSearchServer{
		authToken: authToken,
		logger:    logger,
	}
}

// RegisterProvider sets the vector search provider plugin's client connection.
func (s *VectorSearchServer) RegisterProvider(name string, client protocol.VectorSearchServiceClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = client
	s.name = name
	s.logger.Debugw("VectorSearch provider registered", "provider", name)
}

// HasProvider returns true if a vector search provider is registered.
func (s *VectorSearchServer) HasProvider() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.provider != nil
}

// Search finds the nearest neighbors to a query vector in a named index.
func (s *VectorSearchServer) Search(ctx context.Context, req *protocol.VectorSearchRequest) (*protocol.VectorSearchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	client, _, err := s.getProvider()
	if err != nil {
		return nil, err
	}

	if req.Index == "" {
		return nil, fmt.Errorf("index name cannot be empty")
	}
	if len(req.QueryVector) == 0 {
		return nil, fmt.Errorf("query vector cannot be empty")
	}

	return client.Search(ctx, req)
}

// AddVectors inserts vectors into a named index.
func (s *VectorSearchServer) AddVectors(ctx context.Context, req *protocol.AddVectorsRequest) (*protocol.AddVectorsResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	client, _, err := s.getProvider()
	if err != nil {
		return nil, err
	}

	if req.Index == "" {
		return nil, fmt.Errorf("index name cannot be empty")
	}
	if len(req.Vectors) == 0 {
		return nil, fmt.Errorf("no vectors provided")
	}

	return client.AddVectors(ctx, req)
}

// CreateIndex creates a new named vector index.
func (s *VectorSearchServer) CreateIndex(ctx context.Context, req *protocol.CreateIndexRequest) (*protocol.CreateIndexResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return nil, err
	}

	client, _, err := s.getProvider()
	if err != nil {
		return nil, err
	}

	if req.Name == "" {
		return nil, fmt.Errorf("index name cannot be empty")
	}
	if req.Dimensions <= 0 {
		return nil, fmt.Errorf("dimensions must be positive, got %d", req.Dimensions)
	}

	return client.CreateIndex(ctx, req)
}

// getProvider returns the vector search client or an error if none is registered.
func (s *VectorSearchServer) getProvider() (protocol.VectorSearchServiceClient, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.provider == nil {
		return nil, "", status.Error(codes.Unavailable, "no vector search provider registered")
	}

	return s.provider, s.name, nil
}
