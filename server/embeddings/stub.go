//go:build !rustembeddings || !cgo

package embeddings

import "net/http"

const unavailableMsg = "Embeddings feature not available (compile with -tags=rustembeddings)"

// HandleClusterMemberships is a no-op when embeddings are not available.
func (h *Handler) HandleClusterMemberships(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleEmbeddingClusters is a no-op when embeddings are not available.
func (h *Handler) HandleEmbeddingClusters(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleClusterSamples is a no-op when embeddings are not available.
func (h *Handler) HandleClusterSamples(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleClusterMembers is a no-op when embeddings are not available.
func (h *Handler) HandleClusterMembers(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleClusterTimeline is a no-op when embeddings are not available.
func (h *Handler) HandleClusterTimeline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`[]`))
}

// HandleSemanticSearch is a no-op when embeddings are not available.
func (h *Handler) HandleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleEmbeddingGenerate is a no-op when embeddings are not available.
func (h *Handler) HandleEmbeddingGenerate(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleEmbeddingBatch is a no-op when embeddings are not available.
func (h *Handler) HandleEmbeddingBatch(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleEmbeddingsBySource is a no-op when embeddings are not available.
func (h *Handler) HandleEmbeddingsBySource(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleEmbeddingInfo is a no-op when embeddings are not available.
func (h *Handler) HandleEmbeddingInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"available":false,"model_name":"","dimensions":0,"embedding_count":0,"attestation_count":0}`))
}

// HandleEmbeddingProjections is a no-op when embeddings are not available.
func (h *Handler) HandleEmbeddingProjections(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleCluster is a no-op when embeddings are not available.
func (h *Handler) HandleCluster(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}

// HandleProject is a no-op when embeddings are not available.
func (h *Handler) HandleProject(w http.ResponseWriter, r *http.Request) {
	http.Error(w, unavailableMsg, http.StatusServiceUnavailable)
}
