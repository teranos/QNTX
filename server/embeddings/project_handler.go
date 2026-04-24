//go:build cgo && rustembeddings

package embeddings

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	appcfg "github.com/teranos/QNTX/am"
)

// HandleProject runs configured projection methods on all embeddings.
// POST /api/embeddings/project
func (h *Handler) HandleProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Service == nil || h.Store == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	methods := appcfg.GetStringSlice("embeddings.projection_methods")
	if len(methods) == 0 {
		methods = []string{"umap"}
	}

	var params *ProjectionParams
	if r.Body != nil {
		var req ProjectionParams
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.NNeighbors != nil || req.MinDist != nil || req.Perplexity != nil {
				params = &req
			}
		}
	}

	startTime := time.Now()
	results, err := RunAllProjections(r.Context(), methods, h.Store, h.Service, h.CallReduce, h.Logger, params)
	if err != nil {
		h.Logger.Errorw("Projection failed", "methods", methods, "error", err)
		http.Error(w, fmt.Sprintf("Projection failed: %s", err), http.StatusInternalServerError)
		return
	}

	totalMS := float64(time.Since(startTime).Milliseconds())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results":  results,
		"total_ms": totalMS,
	})
}
