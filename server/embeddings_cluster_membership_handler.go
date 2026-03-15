//go:build cgo && rustembeddings

package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// HandleClusterMemberships returns cluster assignments for a list of attestation IDs.
// GET /api/embeddings/clusters/memberships?ids=ID1,ID2,...
// Response: { "memberships": { "ID1": { "cluster_id": 3, "label": "Technical code review" }, ... } }
func (s *QNTXServer) HandleClusterMemberships(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	raw := r.URL.Query().Get("ids")
	if raw == "" {
		http.Error(w, "ids parameter required (comma-separated attestation IDs)", http.StatusBadRequest)
		return
	}

	ids := strings.Split(raw, ",")

	memberships, err := s.embeddingStore.GetClusterMemberships(ids)
	if err != nil {
		s.logger.Errorw("Failed to get cluster memberships", "n_ids", len(ids), "error", err)
		http.Error(w, "Failed to retrieve cluster memberships", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"memberships": memberships}); err != nil {
		s.logger.Errorw("Failed to encode cluster memberships response", "error", err)
	}
}
