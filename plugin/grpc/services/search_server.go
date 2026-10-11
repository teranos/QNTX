package services

import (
	"context"
	"sync"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
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

	mu        sync.RWMutex
	providing searchProviding
	logger    *zap.SugaredLogger
}

// searchProviding is who answers search: a registered provider, or none.
type searchProviding interface {
	providesSearch()
}

// searchProvider is the registered provider, and its name.
type searchProvider struct {
	client protocol.SearchServiceClient
	name   string
}

// noSearch is no provider registered.
type noSearch struct{}

func (searchProvider) providesSearch() {}
func (noSearch) providesSearch()       {}

// noSearchProvider is the search server with no provider registered.
func noSearchProvider() error {
	return status.Error(codes.Unavailable, "no search provider registered")
}

// NewSearchServer creates a new search routing server. Starts empty — provider registers after init.
func NewSearchServer(logger *zap.SugaredLogger) *SearchServer {
	return &SearchServer{
		providing: noSearch{},
		logger:    logger,
	}
}

// RegisterProvider sets the search provider plugin's client connection.
func (s *SearchServer) RegisterProvider(name string, client protocol.SearchServiceClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.providing = searchProvider{client: client, name: name}
	s.logger.Debugw("Search provider registered", "provider", name)
}

// UnregisterProvider clears the search provider, making HasProvider() return false.
// Called when a search provider plugin is disabled via hot-swap.
func (s *SearchServer) UnregisterProvider(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch registered := s.providing.(type) {
	case searchProvider:
		if registered.name == name {
			s.clear()
			s.logger.Debugw("Search provider unregistered", "provider", name)
		}
	case noSearch:
	}
}

// ClearProviders removes all providers. Called during server shutdown
// to prevent observers from routing to dead gRPC connections.
func (s *SearchServer) ClearProviders() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clear()
}

// clear forgets the provider (must be called with lock held).
func (s *SearchServer) clear() {
	s.providing = noSearch{}
}

// HasProvider returns true if a search provider is registered.
func (s *SearchServer) HasProvider() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch s.providing.(type) {
	case searchProvider:
		return true
	case noSearch:
	}
	return false
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

	switch providing := s.providing.(type) {
	case searchProvider:
		return providing.client, providing.name, nil
	case noSearch:
		return nil, "", noSearchProvider()
	}
	return nil, "", errors.Newf("search names neither a provider nor that it has none: %T", s.providing)
}
