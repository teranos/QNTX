//go:build meilisearch

package grpc

import (
	"context"
	"fmt"
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/search"
	"go.uber.org/zap"
)

// SearchServer implements the SearchService gRPC server.
// Starts with no backend — SetService must be called once Meilisearch
// is initialized (same lazy pattern as EmbeddingServer).
type SearchServer struct {
	protocol.UnimplementedSearchServiceServer
	mu        sync.RWMutex
	service   *search.Service
	store     ats.AttestationStore // for reindex
	authToken string
	logger    *zap.SugaredLogger
}

// NewSearchServer creates a new search gRPC server.
func NewSearchServer(authToken string, logger *zap.SugaredLogger) *SearchServer {
	return &SearchServer{
		authToken: authToken,
		logger:    logger,
	}
}

// SetService registers the search backend and attestation store after initialization.
func (s *SearchServer) SetService(svc *search.Service, store ats.AttestationStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = svc
	s.store = store
	s.logger.Infow("Search gRPC service backend registered")
}

// Search performs a full-text search over indexed attestations.
func (s *SearchServer) Search(ctx context.Context, req *protocol.SearchRequest) (*protocol.SearchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.SearchResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.mu.RLock()
	svc := s.service
	s.mu.RUnlock()

	if svc == nil {
		return &protocol.SearchResponse{
			Success: false,
			Error:   "meilisearch service not initialized",
		}, nil
	}

	filters := search.SearchFilters{
		Subjects:   req.Subjects,
		Predicates: req.Predicates,
		Contexts:   req.Contexts,
		Actors:     req.Actors,
		Source:     req.Source,
		Limit:      int(req.Limit),
		Offset:     int(req.Offset),
	}
	if req.TimeStart != nil {
		ts := *req.TimeStart
		filters.TimeStart = &ts
	}
	if req.TimeEnd != nil {
		te := *req.TimeEnd
		filters.TimeEnd = &te
	}

	result, err := svc.Search(req.Query, filters)
	if err != nil {
		return &protocol.SearchResponse{
			Success: false,
			Error:   fmt.Sprintf("search failed: %v", err),
		}, nil
	}

	hits := make([]*protocol.SearchHit, len(result.Hits))
	for i, h := range result.Hits {
		hits[i] = &protocol.SearchHit{
			AttestationId: h.AttestationID,
			Score:         h.Score,
		}
	}

	return &protocol.SearchResponse{
		Success:          true,
		Query:            result.Query,
		Hits:             hits,
		TotalHits:        result.TotalHits,
		ProcessingTimeMs: result.ProcessingTimeMS,
	}, nil
}

// Reindex triggers a full reindex of all attestations.
func (s *SearchServer) Reindex(ctx context.Context, req *protocol.ReindexRequest) (*protocol.ReindexResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.ReindexResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.mu.RLock()
	svc := s.service
	store := s.store
	s.mu.RUnlock()

	if svc == nil || store == nil {
		return &protocol.ReindexResponse{
			Success: false,
			Error:   "meilisearch service not initialized",
		}, nil
	}

	count, err := svc.Reindex(store)
	if err != nil {
		return &protocol.ReindexResponse{
			Success: false,
			Error:   fmt.Sprintf("reindex failed: %v", err),
		}, nil
	}

	return &protocol.ReindexResponse{
		Success:          true,
		DocumentsIndexed: int32(count),
	}, nil
}

// Stats returns index statistics.
func (s *SearchServer) Stats(ctx context.Context, req *protocol.SearchStatsRequest) (*protocol.SearchStatsResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.SearchStatsResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.mu.RLock()
	svc := s.service
	s.mu.RUnlock()

	if svc == nil {
		return &protocol.SearchStatsResponse{
			Success: false,
			Error:   "meilisearch service not initialized",
		}, nil
	}

	stats, err := svc.Stats()
	if err != nil {
		return &protocol.SearchStatsResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get stats: %v", err),
		}, nil
	}

	var numDocs int64
	if v, ok := stats["number_of_documents"].(int64); ok {
		numDocs = v
	}
	var isIndexing bool
	if v, ok := stats["is_indexing"].(bool); ok {
		isIndexing = v
	}

	return &protocol.SearchStatsResponse{
		Success:           true,
		NumberOfDocuments: numDocs,
		IsIndexing:        isIndexing,
	}, nil
}
