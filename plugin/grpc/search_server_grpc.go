//go:build meilisearch

package grpc

// gRPC method implementations for SearchServer.
// Gated behind //go:build meilisearch until make proto generates
// the SearchServiceServer interface and message types.
//
// After running make proto:
//  1. Remove the build tag from this file
//  2. Embed protocol.UnimplementedSearchServiceServer in SearchServer
//  3. Uncomment SearchEndpoint in client.go InitializeRequest
//  4. Add startSearchService() to ServicesManager.Start()

import (
	"context"
	"fmt"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/search"
)

// Search performs a full-text search over indexed attestations.
func (s *SearchServer) Search(ctx context.Context, req *protocol.SearchRequest) (*protocol.SearchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.AuthToken); err != nil {
		return &protocol.SearchResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.mu.RLock()
	svc := s.Service
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
	if err := ValidateToken(req.AuthToken, s.AuthToken); err != nil {
		return &protocol.ReindexResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.mu.RLock()
	svc := s.Service
	store := s.Store
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
	if err := ValidateToken(req.AuthToken, s.AuthToken); err != nil {
		return &protocol.SearchStatsResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.mu.RLock()
	svc := s.Service
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
