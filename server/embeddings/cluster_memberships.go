//go:build cgo && rustembeddings

package embeddings

import (
	"encoding/json"
	"net/http"
	"strings"
)

// HandleClusterMemberships returns cluster assignments for a list of attestation IDs.
// GET /api/embeddings/clusters/memberships?ids=ID1,ID2,...
func (h *Handler) HandleClusterMemberships(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Store == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	raw := r.URL.Query().Get("ids")
	if raw == "" {
		http.Error(w, "ids parameter required (comma-separated attestation IDs)", http.StatusBadRequest)
		return
	}

	ids := strings.Split(raw, ",")

	memberships, err := h.Store.GetClusterMemberships(ids)
	if err != nil {
		h.Logger.Errorw("Failed to get cluster memberships", "n_ids", len(ids), "error", err)
		http.Error(w, "Failed to retrieve cluster memberships", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"memberships": memberships}); err != nil {
		h.Logger.Errorw("Failed to encode cluster memberships response", "error", err)
	}
}
