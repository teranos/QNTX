package grpc

import (
	"sync"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/search"
	"go.uber.org/zap"
)

// SearchServer holds the Meilisearch backend for gRPC plugin access.
// Starts with no backend — SetService must be called once Meilisearch
// is initialized (same lazy pattern as EmbeddingServer).
//
// gRPC method implementations (Search, Reindex, Stats) are in
// search_server_grpc.go, gated behind //go:build meilisearch until
// make proto generates the SearchServiceServer interface.
type SearchServer struct {
	mu        sync.RWMutex
	Service   *search.Service
	Store     ats.AttestationStore // for reindex
	AuthToken string
	Logger    *zap.SugaredLogger
}

// NewSearchServer creates a new search gRPC server.
func NewSearchServer(authToken string, logger *zap.SugaredLogger) *SearchServer {
	return &SearchServer{
		AuthToken: authToken,
		Logger:    logger,
	}
}

// SetService registers the search backend and attestation store after initialization.
func (s *SearchServer) SetService(svc *search.Service, store ats.AttestationStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Service = svc
	s.Store = store
	s.Logger.Infow("Search gRPC service backend registered")
}
