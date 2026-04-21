//go:build !rustembeddings || !cgo

package server

import (
	"net/http"

	am "github.com/teranos/QNTX/am"
)

// Stub handlers when rustembeddings build tag is not present

// HandleEmbeddingCluster runs HDBSCAN clustering (POST /api/embeddings/cluster)
func (s *QNTXServer) HandleEmbeddingCluster(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// HandleEmbeddingProject runs UMAP projection (POST /api/embeddings/project)
func (s *QNTXServer) HandleEmbeddingProject(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// SetupEmbeddingService is a no-op when embeddings are not available
func (s *QNTXServer) SetupEmbeddingService() {
	s.logger.Debugw("Embeddings service not available (build without rustembeddings tag)")
}

// setupEmbeddingReclusterSchedule is a no-op when embeddings are not available
func (s *QNTXServer) setupEmbeddingReclusterSchedule(cfg *am.Config) {}

// setupEmbeddingReprojectSchedule is a no-op when embeddings are not available
func (s *QNTXServer) setupEmbeddingReprojectSchedule(cfg *am.Config) {}

// setupClusterLabelSchedule is a no-op when embeddings are not available
func (s *QNTXServer) setupClusterLabelSchedule(cfg *am.Config) {}

// hasRustEmbeddings returns false when compiled without rustembeddings build tag
func hasRustEmbeddings() bool {
	return false
}
