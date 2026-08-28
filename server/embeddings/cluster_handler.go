package embeddings

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/projectctx"
)

// ClusterRequest represents a clustering API request.
type ClusterRequest struct {
	MinClusterSize        int      `json:"min_cluster_size,omitempty"`
	ClusterThreshold      *float64 `json:"cluster_threshold,omitempty"`
	ClusterMatchThreshold *float64 `json:"cluster_match_threshold,omitempty"`
}

// ClusterResponse represents the result of a clustering operation.
type ClusterResponse struct {
	Summary *storage.ClusterSummary `json:"summary"`
	TimeMS  float64                 `json:"time_ms"`

	// Set when the run used values that did not save, so a 200 cannot be read
	// as "these settings are now in effect".
	Warning string `json:"warning,omitempty"`
}

// HandleCluster runs HDBSCAN clustering on all stored embeddings (POST /api/embeddings/cluster).
func (h *Handler) HandleCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Service == nil || h.Store == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	if h.ClusterFunc == nil {
		http.Error(w, "Clustering not available (no embedding provider plugin)", http.StatusServiceUnavailable)
		return
	}

	// Parse optional clustering parameters from body
	minClusterSize := appcfg.GetInt("embeddings.min_cluster_size")
	if minClusterSize <= 0 {
		minClusterSize = 5
	}
	clusterMatchThreshold := appcfg.GetFloat64("embeddings.cluster_match_threshold")

	var req ClusterRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.MinClusterSize > 0 {
				minClusterSize = req.MinClusterSize
			}
			if req.ClusterMatchThreshold != nil && *req.ClusterMatchThreshold > 0 {
				clusterMatchThreshold = *req.ClusterMatchThreshold
			}
		}
	}

	projectCtx := projectctx.Namespace()

	model := r.URL.Query().Get("model")

	result, err := RunHDBSCANClustering(
		h.Store,
		h.Service,
		h.ClusterFunc,
		h.Invalidator,
		minClusterSize,
		clusterMatchThreshold,
		h.ATSStore,
		projectCtx,
		h.GroundDBPath,
		h.GroundWrite,
		h.Logger,
		model,
	)
	if err != nil {
		h.Logger.Errorw("HDBSCAN clustering failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Persist parameters only after successful clustering. A setting that ran
	// but did not save reverts on the next run, and a 200 alone says it stuck.
	var unsaved []string
	if req.MinClusterSize > 0 {
		if err := appcfg.UpdateEmbeddingsMinClusterSize(req.MinClusterSize); err != nil {
			h.Logger.Errorw("Failed to persist min_cluster_size", "error", err)
			unsaved = append(unsaved, "min_cluster_size: "+err.Error())
		}
	}
	if req.ClusterThreshold != nil {
		if err := appcfg.UpdateEmbeddingsClusterThreshold(*req.ClusterThreshold); err != nil {
			h.Logger.Errorw("Failed to persist cluster_threshold", "error", err)
			unsaved = append(unsaved, "cluster_threshold: "+err.Error())
		}
	}
	if req.ClusterMatchThreshold != nil {
		if err := appcfg.UpdateEmbeddingsClusterMatchThreshold(*req.ClusterMatchThreshold); err != nil {
			h.Logger.Errorw("Failed to persist cluster_match_threshold", "error", err)
			unsaved = append(unsaved, "cluster_match_threshold: "+err.Error())
		}
	}

	resp := ClusterResponse{
		Summary: result.Summary,
		TimeMS:  result.TimeMS,
	}
	if len(unsaved) > 0 {
		resp.Warning = "clustering ran with these values but they were not saved and will revert: " +
			strings.Join(unsaved, "; ")
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.Logger.Errorw("Failed to encode cluster response", "error", err)
	}
}
