//go:build !rustembeddings || !cgo

package server

import "net/http"

// Stub handlers when rustembeddings build tag is not present

// HandleSemanticSearch handles semantic search requests (GET /api/search/semantic)
func (s *QNTXServer) HandleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// HandleEmbeddingGenerate handles embedding generation requests (POST /api/embeddings/generate)
func (s *QNTXServer) HandleEmbeddingGenerate(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// HandleEmbeddingBatch handles batch embedding generation (POST /api/embeddings/batch)
func (s *QNTXServer) HandleEmbeddingBatch(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// HandleEmbeddingInfo returns embedding service status (GET /api/embeddings/info)
func (s *QNTXServer) HandleEmbeddingInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"available":false,"model_name":"","dimensions":0,"embedding_count":0,"attestation_count":0}`))
}

// HandleEmbeddingCluster runs HDBSCAN clustering (POST /api/embeddings/cluster)
func (s *QNTXServer) HandleEmbeddingCluster(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// HandleEmbeddingProject runs UMAP projection (POST /api/embeddings/project)
func (s *QNTXServer) HandleEmbeddingProject(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// HandleEmbeddingProjections serves 2D projections (GET /api/embeddings/projections)
func (s *QNTXServer) HandleEmbeddingProjections(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Embeddings feature not available (compile with -tags=rustembeddings)", http.StatusServiceUnavailable)
}

// SetupEmbeddingService is a no-op when embeddings are not available
func (s *QNTXServer) SetupEmbeddingService() {
	s.logger.Debugw("Embeddings service not available (build without rustembeddings tag)")
}

// hasRustEmbeddings returns false when compiled without rustembeddings build tag
func hasRustEmbeddings() bool {
	return false
}
