package grpc

import (
	"context"
	"sync"

	"github.com/teranos/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SearchServer is the core-side gRPC server that routes search requests to the provider plugin.
// It implements protocol.SearchServiceServer and holds a SearchServiceClient connection
// to the provider plugin (qntx-meili) that registered a SearchService on its gRPC server.
//
// Starts empty — the provider registers after init, same pattern as LLMServer.
type SearchServer struct {
	protocol.UnimplementedSearchServiceServer

	mu       sync.RWMutex
	provider protocol.SearchServiceClient
	name     string // provider name for logging
	logger   *zap.SugaredLogger
}

// NewSearchServer creates a new search routing server. Starts empty — provider registers after init.
func NewSearchServer(logger *zap.SugaredLogger) *SearchServer {
	return &SearchServer{
		logger: logger,
	}
}

// RegisterProvider sets the search provider plugin's client connection.
func (s *SearchServer) RegisterProvider(name string, client protocol.SearchServiceClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.provider = client
	s.name = name
	s.logger.Debugw("Search provider registered", "provider", name)
}

// UnregisterProvider clears the search provider, making HasProvider() return false.
// Called when a search provider plugin is disabled via hot-swap.
func (s *SearchServer) UnregisterProvider(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.name == name {
		s.provider = nil
		s.name = ""
		s.logger.Debugw("Search provider unregistered", "provider", name)
	}
}

// ClearProviders removes all providers. Called during server shutdown
// to prevent observers from routing to dead gRPC connections.
func (s *SearchServer) ClearProviders() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = nil
	s.name = ""
}

// HasProvider returns true if a search provider is registered.
func (s *SearchServer) HasProvider() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.provider != nil
}

// Search routes a search request to the provider plugin.
func (s *SearchServer) Search(ctx context.Context, req *protocol.SearchRequest) (*protocol.SearchResponse, error) {
	client, name, err := s.getProvider()
	if err != nil {
		return nil, err
	}

	resp, err := client.Search(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "search via provider %s failed", name)
	}

	return resp, nil
}

// IndexDocuments routes an index request to the provider plugin.
func (s *SearchServer) IndexDocuments(ctx context.Context, req *protocol.IndexDocumentsRequest) (*protocol.IndexDocumentsResponse, error) {
	client, name, err := s.getProvider()
	if err != nil {
		return nil, err
	}

	resp, err := client.IndexDocuments(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "index documents via provider %s failed", name)
	}

	return resp, nil
}

// DeleteDocuments routes a delete request to the provider plugin.
func (s *SearchServer) DeleteDocuments(ctx context.Context, req *protocol.DeleteDocumentsRequest) (*protocol.DeleteDocumentsResponse, error) {
	client, name, err := s.getProvider()
	if err != nil {
		return nil, err
	}

	resp, err := client.DeleteDocuments(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "delete documents via provider %s failed", name)
	}

	return resp, nil
}

// ConfigureIndex routes an index configuration request to the provider plugin.
func (s *SearchServer) ConfigureIndex(ctx context.Context, req *protocol.ConfigureIndexRequest) (*protocol.ConfigureIndexResponse, error) {
	client, name, err := s.getProvider()
	if err != nil {
		return nil, err
	}

	resp, err := client.ConfigureIndex(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "configure index via provider %s failed", name)
	}

	return resp, nil
}

// getProvider returns the search client or an error if none is registered.
func (s *SearchServer) getProvider() (protocol.SearchServiceClient, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.provider == nil {
		return nil, "", status.Error(codes.Unavailable, "no search provider registered")
	}

	return s.provider, s.name, nil
}
