//go:build cgo && rustembeddings

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

// ClusterRequest represents a clustering API request
type ClusterRequest struct {
	MinClusterSize int `json:"min_cluster_size,omitempty"`
}

// ClusterResponse represents the result of a clustering operation
type ClusterResponse struct {
	Summary *storage.ClusterSummary `json:"summary"`
	TimeMS  float64                 `json:"time_ms"`
}

// ClusterListEntry represents a single cluster in the API response.
type ClusterListEntry struct {
	ID        int     `json:"id"`
	Label     *string `json:"label"`
	Members   int     `json:"members"`
	Status    string  `json:"status"`
	FirstSeen string  `json:"first_seen"`
	LastSeen  string  `json:"last_seen"`
}

// HandleEmbeddingCluster runs HDBSCAN clustering on all stored embeddings (POST /api/embeddings/cluster)
func (s *QNTXServer) HandleEmbeddingCluster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingService == nil || s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	// Parse optional min_cluster_size from body
	minClusterSize := 5
	if r.Body != nil {
		var req ClusterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.MinClusterSize > 0 {
			minClusterSize = req.MinClusterSize
		}
	}

	cwd, _ := os.Getwd()
	projectCtx := "project:" + filepath.Join(filepath.Base(filepath.Dir(cwd)), filepath.Base(cwd))

	result, err := RunHDBSCANClustering(
		s.embeddingStore,
		s.embeddingService,
		s.embeddingClusterInvalidator,
		minClusterSize,
		appcfg.GetFloat64("embeddings.cluster_match_threshold"),
		s.atsStore,
		projectCtx,
		s.logger,
	)
	if err != nil {
		s.logger.Errorw("HDBSCAN clustering failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ClusterResponse{
		Summary: result.Summary,
		TimeMS:  result.TimeMS,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Errorw("Failed to encode cluster response", "error", err)
	}
}

// HandleEmbeddingClusters returns active clusters with metadata (GET /api/embeddings/clusters)
func (s *QNTXServer) HandleEmbeddingClusters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	details, err := s.embeddingStore.GetClusterDetails()
	if err != nil {
		s.logger.Errorw("Failed to get cluster details", "error", err)
		http.Error(w, "Failed to retrieve cluster details", http.StatusInternalServerError)
		return
	}

	entries := make([]ClusterListEntry, len(details))
	for i, d := range details {
		entries[i] = ClusterListEntry{
			ID:        d.ID,
			Label:     d.Label,
			Members:   d.Members,
			Status:    d.Status,
			FirstSeen: d.FirstSeen.Format(time.RFC3339),
			LastSeen:  d.LastSeen.Format(time.RFC3339),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		s.logger.Errorw("Failed to encode clusters response", "error", err)
	}
}

// HandleClusterSamples returns sample texts from a cluster (GET /api/embeddings/clusters/samples?cluster_id=N&size=5)
func (s *QNTXServer) HandleClusterSamples(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	clusterID, err := strconv.Atoi(r.URL.Query().Get("cluster_id"))
	if err != nil {
		http.Error(w, "cluster_id parameter required (integer)", http.StatusBadRequest)
		return
	}

	size := 5
	if sizeParam := r.URL.Query().Get("size"); sizeParam != "" {
		if n, err := strconv.Atoi(sizeParam); err == nil && n > 0 && n <= 20 {
			size = n
		}
	}

	samples, err := s.embeddingStore.SampleClusterTexts(clusterID, size)
	if err != nil {
		s.logger.Errorw("Failed to sample cluster texts", "cluster_id", clusterID, "error", err)
		http.Error(w, fmt.Sprintf("Failed to sample texts for cluster %d", clusterID), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"cluster_id": clusterID, "samples": samples})
}

// HandleClusterMembers returns recent attestations belonging to a cluster (GET /api/embeddings/clusters/members?cluster_id=N&limit=20)
func (s *QNTXServer) HandleClusterMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	clusterID, err := strconv.Atoi(r.URL.Query().Get("cluster_id"))
	if err != nil {
		http.Error(w, "cluster_id parameter required (integer)", http.StatusBadRequest)
		return
	}

	limit := 20
	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		if n, err := strconv.Atoi(limitParam); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	sourceIDs, err := s.embeddingStore.GetClusterMemberIDs(clusterID, limit)
	if err != nil {
		s.logger.Errorw("Failed to get cluster member IDs", "cluster_id", clusterID, "error", err)
		http.Error(w, fmt.Sprintf("Failed to get members for cluster %d", clusterID), http.StatusInternalServerError)
		return
	}

	attestations := make([]*types.As, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		as, err := storage.GetAttestationByID(s.db, id)
		if err != nil {
			s.logger.Warnw("Failed to resolve cluster member attestation", "source_id", id, "error", err)
			continue
		}
		if as != nil {
			attestations = append(attestations, as)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"cluster_id": clusterID, "attestations": attestations})
}

// HandleClusterTimeline serves cluster evolution data across runs (GET /api/embeddings/cluster-timeline).
func (s *QNTXServer) HandleClusterTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	timeline, err := s.embeddingStore.GetClusterTimeline()
	if err != nil {
		s.logger.Errorw("Failed to get cluster timeline", "error", err)
		http.Error(w, "Failed to retrieve cluster timeline", http.StatusInternalServerError)
		return
	}

	if timeline == nil {
		timeline = []storage.ClusterTimelinePoint{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(timeline); err != nil {
		s.logger.Errorw("Failed to encode cluster timeline response", "error", err)
	}
}
