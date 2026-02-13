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

// SetupEmbeddingService is a no-op when embeddings are not available
func (s *QNTXServer) SetupEmbeddingService() {
	s.logger.Debugw("Embeddings service not available (build without rustembeddings tag)")
}

// hasRustEmbeddings returns false when compiled without rustembeddings build tag
func hasRustEmbeddings() bool {
	return false
}
