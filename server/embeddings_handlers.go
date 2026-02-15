//go:build cgo && rustembeddings

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// SemanticSearchRequest represents a semantic search API request
type SemanticSearchRequest struct {
	Query     string  `json:"q"`
	Limit     int     `json:"limit,omitempty"`
	Threshold float32 `json:"threshold,omitempty"`
}

// SemanticSearchResponse represents a semantic search API response
type SemanticSearchResponse struct {
	Query   string                 `json:"query"`
	Results []SemanticSearchResult `json:"results"`
	Stats   SemanticSearchStats    `json:"stats"`
}

// SemanticSearchResult represents a single search result
type SemanticSearchResult struct {
	Attestation *types.As `json:"attestation"`
	Similarity  float32   `json:"similarity"`
	Distance    float32   `json:"distance"`
}

// SemanticSearchStats provides search statistics
type SemanticSearchStats struct {
	TotalResults int     `json:"total_results"`
	InferenceMS  float64 `json:"inference_ms"`
	SearchMS     float64 `json:"search_ms"`
}

// HandleSemanticSearch handles semantic search requests (GET /api/search/semantic)
func (s *QNTXServer) HandleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if embeddings service is available
	if s.embeddingService == nil || s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Query parameter 'q' is required", http.StatusBadRequest)
		return
	}

	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	threshold := float32(0.7)
	if thresholdStr := r.URL.Query().Get("threshold"); thresholdStr != "" {
		if parsedThreshold, err := strconv.ParseFloat(thresholdStr, 32); err == nil {
			threshold = float32(parsedThreshold)
		}
	}

	var clusterID *int
	if cidStr := r.URL.Query().Get("cluster_id"); cidStr != "" {
		if parsedCID, err := strconv.Atoi(cidStr); err == nil {
			clusterID = &parsedCID
		}
	}

	// Generate embedding for query
	startInference := time.Now()
	queryResult, err := s.embeddingService.GenerateEmbedding(query)
	if err != nil {
		s.logger.Errorw("Failed to generate query embedding",
			"query", query,
			"error", err)
		http.Error(w, "Failed to generate query embedding", http.StatusInternalServerError)
		return
	}
	inferenceMS := time.Since(startInference).Milliseconds()

	// Serialize embedding for sqlite-vec
	queryBlob, err := s.embeddingService.SerializeEmbedding(queryResult.Embedding)
	if err != nil {
		s.logger.Errorw("Failed to serialize query embedding",
			"query", query,
			"dimensions", len(queryResult.Embedding),
			"error", err)
		http.Error(w, "Failed to serialize embedding", http.StatusInternalServerError)
		return
	}

	// Perform semantic search
	startSearch := time.Now()
	searchResults, err := s.embeddingStore.SemanticSearch(queryBlob, limit, threshold, clusterID)
	if err != nil {
		s.logger.Errorw("Failed to perform semantic search",
			"query", query,
			"limit", limit,
			"threshold", threshold,
			"error", err)
		http.Error(w, "Failed to perform search", http.StatusInternalServerError)
		return
	}
	searchMS := time.Since(startSearch).Milliseconds()

	// Fetch attestations for results
	response := SemanticSearchResponse{
		Query:   query,
		Results: make([]SemanticSearchResult, 0, len(searchResults)),
		Stats: SemanticSearchStats{
			TotalResults: len(searchResults),
			InferenceMS:  float64(inferenceMS),
			SearchMS:     float64(searchMS),
		},
	}

	for _, result := range searchResults {
		if result.SourceType == "attestation" {
			attestation, err := storage.GetAttestationByID(s.db, result.SourceID)
			if err != nil {
				s.logger.Warnw("Failed to fetch attestation for search result",
					"attestation_id", result.SourceID,
					"error", err)
				continue
			}
			if attestation == nil {
				continue
			}

			response.Results = append(response.Results, SemanticSearchResult{
				Attestation: attestation,
				Similarity:  result.Similarity,
				Distance:    result.Distance,
			})
		}
	}

	response.Stats.TotalResults = len(response.Results)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode semantic search response",
			"result_count", len(response.Results),
			"error", err)
	}
}

// EmbeddingGenerateRequest represents an embedding generation request
type EmbeddingGenerateRequest struct {
	Text string `json:"text"`
}

// EmbeddingGenerateResponse represents an embedding generation response
type EmbeddingGenerateResponse struct {
	Embedding   []float32 `json:"embedding"`
	Dimensions  int       `json:"dimensions"`
	Model       string    `json:"model"`
	Tokens      int       `json:"tokens"`
	InferenceMS float64   `json:"inference_ms"`
}

// HandleEmbeddingGenerate handles embedding generation requests (POST /api/embeddings/generate)
func (s *QNTXServer) HandleEmbeddingGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if embeddings service is available
	if s.embeddingService == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	// Parse request body
	var req EmbeddingGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "Text field is required", http.StatusBadRequest)
		return
	}

	// Generate embedding
	result, err := s.embeddingService.GenerateEmbedding(req.Text)
	if err != nil {
		s.logger.Errorw("Failed to generate embedding",
			"text_length", len(req.Text),
			"error", err)
		http.Error(w, "Failed to generate embedding", http.StatusInternalServerError)
		return
	}

	// Get model info
	modelInfo, err := s.embeddingService.GetModelInfo()
	if err != nil {
		s.logger.Errorw("Failed to get model info",
			"error", err)
		modelInfo = &embeddings.ModelInfo{
			Name:       "all-MiniLM-L6-v2",
			Dimensions: 384,
		}
	}

	// Send response
	response := EmbeddingGenerateResponse{
		Embedding:   result.Embedding,
		Dimensions:  modelInfo.Dimensions,
		Model:       modelInfo.Name,
		Tokens:      result.Tokens,
		InferenceMS: result.InferenceMS,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode embedding response",
			"dimensions", response.Dimensions,
			"error", err)
	}
}

// EmbeddingBatchRequest represents a batch embedding request
type EmbeddingBatchRequest struct {
	AttestationIDs []string `json:"attestation_ids"`
}

// EmbeddingBatchResponse represents a batch embedding response
type EmbeddingBatchResponse struct {
	Processed int      `json:"processed"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
	TimeMS    float64  `json:"time_ms"`
}

// HandleEmbeddingBatch handles batch embedding generation (POST /api/embeddings/batch)
func (s *QNTXServer) HandleEmbeddingBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if services are available
	if s.embeddingService == nil || s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	// Parse request body
	var req EmbeddingBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.AttestationIDs) == 0 {
		http.Error(w, "AttestationIDs field is required", http.StatusBadRequest)
		return
	}

	startTime := time.Now()
	processed := 0
	failed := 0
	errorMessages := []string{}

	// Get model info
	modelInfo, err := s.embeddingService.GetModelInfo()
	if err != nil {
		s.logger.Errorw("Failed to get model info",
			"error", err)
		http.Error(w, "Failed to get model info", http.StatusInternalServerError)
		return
	}

	// Prepare batch of embeddings
	embeddingModels := []*storage.EmbeddingModel{}

	// Get rich string fields from type definitions for embedding text construction.
	richStore := storage.NewBoundedStore(s.db, s.logger.Named("embeddings"))
	richFields := richStore.GetDiscoveredRichFields()

	for _, attestationID := range req.AttestationIDs {
		// Check if embedding already exists
		existing, err := s.embeddingStore.GetBySource("attestation", attestationID)
		if err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to check existing embedding for %s",
				attestationID).Error())
			failed++
			continue
		}

		if existing != nil {
			s.logger.Debugw("Embedding already exists, skipping",
				"attestation_id", attestationID)
			processed++
			continue
		}

		// Fetch attestation
		attestation, err := storage.GetAttestationByID(s.db, attestationID)
		if err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to fetch attestation %s",
				attestationID).Error())
			failed++
			continue
		}
		if attestation == nil {
			errorMessages = append(errorMessages, errors.Newf("attestation %s not found",
				attestationID).Error())
			failed++
			continue
		}

		// Extract rich text only — skip structural-only attestations
		text := extractRichTextFromAttributes(attestation.Attributes, richFields)
		if text == "" {
			errorMessages = append(errorMessages, errors.Newf("attestation %s has no rich text content",
				attestationID).Error())
			failed++
			continue
		}

		// Generate embedding
		result, err := s.embeddingService.GenerateEmbedding(text)
		if err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to generate embedding for %s",
				attestationID).Error())
			failed++
			continue
		}

		// Serialize embedding
		embeddingBlob, err := s.embeddingService.SerializeEmbedding(result.Embedding)
		if err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to serialize embedding for %s",
				attestationID).Error())
			failed++
			continue
		}

		// Create embedding model
		embeddingModel := &storage.EmbeddingModel{
			ID:         "", // Will be auto-generated in storage layer
			SourceType: "attestation",
			SourceID:   attestationID,
			Text:       text,
			Embedding:  embeddingBlob,
			Model:      modelInfo.Name,
			Dimensions: modelInfo.Dimensions,
		}

		embeddingModels = append(embeddingModels, embeddingModel)
		processed++
	}

	// Batch save embeddings
	if len(embeddingModels) > 0 {
		if err := s.embeddingStore.BatchSaveAttestationEmbeddings(embeddingModels); err != nil {
			s.logger.Errorw("Failed to batch save embeddings",
				"count", len(embeddingModels),
				"error", err)
			// Count all as failed
			failed += len(embeddingModels)
			processed -= len(embeddingModels)
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to save %d embeddings to database", len(embeddingModels)).Error())
		}
	}

	// Send response
	response := EmbeddingBatchResponse{
		Processed: processed,
		Failed:    failed,
		TimeMS:    float64(time.Since(startTime).Milliseconds()),
	}

	if len(errorMessages) > 0 {
		response.Errors = errorMessages
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode batch response",
			"processed", response.Processed,
			"failed", response.Failed,
			"error", err)
	}
}

// EmbeddingInfoResponse represents embedding service status
type EmbeddingInfoResponse struct {
	Available        bool                    `json:"available"`
	ModelName        string                  `json:"model_name"`
	Dimensions       int                     `json:"dimensions"`
	EmbeddingCount   int                     `json:"embedding_count"`
	AttestationCount int                     `json:"attestation_count"`
	UnembeddedIDs    []string                `json:"unembedded_ids,omitempty"`
	ClusterInfo      *storage.ClusterSummary `json:"cluster_info,omitempty"`
}

// HandleEmbeddingInfo returns embedding service status and counts (GET /api/embeddings/info)
func (s *QNTXServer) HandleEmbeddingInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := EmbeddingInfoResponse{Available: s.embeddingService != nil}

	if s.embeddingService != nil {
		if info, err := s.embeddingService.GetModelInfo(); err == nil {
			resp.ModelName = info.Name
			resp.Dimensions = info.Dimensions
		}
	}

	// Count embeddings and total attestations
	var embCount, atsCount int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM embeddings").Scan(&embCount); err != nil {
		s.logger.Errorw("Failed to count embeddings", "error", err)
		http.Error(w, "Failed to retrieve embedding count", http.StatusInternalServerError)
		return
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&atsCount); err != nil {
		s.logger.Errorw("Failed to count attestations", "error", err)
		http.Error(w, "Failed to retrieve attestation count", http.StatusInternalServerError)
		return
	}
	resp.EmbeddingCount = embCount
	resp.AttestationCount = atsCount

	// Collect IDs of attestations without embeddings
	rows, err := s.db.Query(`
		SELECT a.id FROM attestations a
		LEFT JOIN embeddings e ON e.source_type = 'attestation' AND e.source_id = a.id
		WHERE e.id IS NULL
	`)
	if err != nil {
		s.logger.Errorw("Failed to query unembedded attestations", "error", err)
		http.Error(w, "Failed to retrieve unembedded attestation list", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			s.logger.Warnw("Failed to scan attestation ID", "error", err)
			continue
		}
		resp.UnembeddedIDs = append(resp.UnembeddedIDs, id)
	}
	if err := rows.Err(); err != nil {
		s.logger.Warnw("Error iterating unembedded attestations", "error", err)
	}

	// Include cluster summary if available
	if s.embeddingStore != nil {
		if summary, err := s.embeddingStore.GetClusterSummary(); err == nil && summary.NClusters > 0 {
			resp.ClusterInfo = summary
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Errorw("Failed to encode embeddings info response",
			"available", resp.Available,
			"embedding_count", resp.EmbeddingCount,
			"attestation_count", resp.AttestationCount,
			"error", err)
	}
}

// ClusterRequest represents the request body for clustering
type ClusterRequest struct {
	MinClusterSize int `json:"min_cluster_size,omitempty"`
}

// ClusterResponse represents the result of a clustering operation
type ClusterResponse struct {
	Summary *storage.ClusterSummary `json:"summary"`
	TimeMS  float64                 `json:"time_ms"`
}

// EmbeddingServiceForClustering is the subset of the embedding service needed for clustering
type EmbeddingServiceForClustering interface {
	DeserializeEmbedding(data []byte) ([]float32, error)
	SerializeEmbedding(embedding []float32) ([]byte, error)
}

// EmbeddingClusterResult holds the outcome of a clustering run
type EmbeddingClusterResult struct {
	Summary *storage.ClusterSummary
	TimeMS  float64
}

// RunHDBSCANClustering executes HDBSCAN on all stored embeddings and writes results to DB.
// Shared by the HTTP handler and the Pulse recluster handler.
func RunHDBSCANClustering(
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	invalidator func(),
	minClusterSize int,
	logger *zap.SugaredLogger,
) (*EmbeddingClusterResult, error) {
	startTime := time.Now()

	// Read all embedding vectors
	ids, blobs, err := store.GetAllEmbeddingVectors()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read embedding vectors for clustering")
	}

	if len(ids) < 2 {
		return nil, errors.Newf("need at least 2 embeddings to cluster, have %d", len(ids))
	}

	// Deserialize blobs into flat float32 array
	var dims int
	flat := make([]float32, 0, len(blobs)*384) // pre-allocate assuming 384d
	for i, blob := range blobs {
		vec, err := svc.DeserializeEmbedding(blob)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to deserialize embedding %s (blob_len=%d)", ids[i], len(blob))
		}
		if i == 0 {
			dims = len(vec)
		}
		flat = append(flat, vec...)
	}

	// Run HDBSCAN
	result, err := embeddings.ClusterHDBSCAN(flat, len(ids), dims, minClusterSize)
	if err != nil {
		return nil, errors.Wrapf(err, "HDBSCAN failed (n_points=%d, dims=%d, min_cluster_size=%d)", len(ids), dims, minClusterSize)
	}

	// Build assignments and write to DB
	assignments := make([]storage.ClusterAssignment, len(ids))
	for i, id := range ids {
		assignments[i] = storage.ClusterAssignment{
			ID:          id,
			ClusterID:   int(result.Labels[i]),
			Probability: float64(result.Probabilities[i]),
		}
	}

	if err := store.UpdateClusterAssignments(assignments); err != nil {
		return nil, errors.Wrapf(err, "failed to save %d cluster assignments", len(assignments))
	}

	// Save cluster centroids for incremental prediction
	if len(result.Centroids) > 0 {
		memberCounts := make(map[int]int)
		for _, l := range result.Labels {
			if l >= 0 {
				memberCounts[int(l)]++
			}
		}

		centroidModels := make([]storage.ClusterCentroid, 0, len(result.Centroids))
		for i, centroid := range result.Centroids {
			blob, err := svc.SerializeEmbedding(centroid)
			if err != nil {
				logger.Errorw("Failed to serialize centroid",
					"cluster_id", i,
					"error", err)
				continue
			}
			centroidModels = append(centroidModels, storage.ClusterCentroid{
				ClusterID: i,
				Centroid:  blob,
				NMembers:  memberCounts[i],
			})
		}

		if err := store.SaveClusterCentroids(centroidModels); err != nil {
			logger.Errorw("Failed to save cluster centroids",
				"count", len(centroidModels),
				"error", err)
			// Non-fatal: clustering succeeded, just centroids not saved
		}

		if invalidator != nil {
			invalidator()
		}
	}

	summary, err := store.GetClusterSummary()
	if err != nil {
		return nil, errors.Wrap(err, "clustering succeeded but failed to read summary")
	}

	timeMS := float64(time.Since(startTime).Milliseconds())

	logger.Infow("HDBSCAN clustering complete",
		"n_points", len(ids),
		"n_clusters", result.NClusters,
		"n_noise", result.NNoise,
		"min_cluster_size", minClusterSize,
		"time_ms", timeMS)

	return &EmbeddingClusterResult{
		Summary: summary,
		TimeMS:  timeMS,
	}, nil
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

	result, err := RunHDBSCANClustering(
		s.embeddingStore,
		s.embeddingService,
		s.embeddingClusterInvalidator,
		minClusterSize,
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

// SetupEmbeddingService initializes the embedding service if available
func (s *QNTXServer) SetupEmbeddingService() {
	// Check for rustembeddings build tag
	if !hasRustEmbeddings() {
		s.logger.Infow("Embeddings service not available (build without rustembeddings tag)")
		return
	}

	// Check if embeddings are enabled in config
	if !appcfg.GetBool("embeddings.enabled") {
		s.logger.Infow("Embeddings service disabled in config (embeddings.enabled=false)")
		return
	}

	// Read model path from config and validate it exists before attempting init
	modelPath := appcfg.GetString("embeddings.path")
	modelName := appcfg.GetString("embeddings.name")

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		s.logger.Errorw("Embeddings enabled but model file not found — set embeddings.path in am.toml",
			"path", modelPath)
		return
	}

	embService, err := embeddings.NewManagedEmbeddingService(modelPath)
	if err != nil {
		s.logger.Errorw("Failed to create embedding service",
			"path", modelPath,
			"error", err)
		return
	}

	// Initialize the service
	if err := embService.Initialize(); err != nil {
		s.logger.Errorw("Failed to initialize embedding service",
			"path", modelPath,
			"error", err)
		return
	}

	// Create embedding store
	embStore := storage.NewEmbeddingStore(s.db, s.logger.Desugar())

	// Store references
	s.embeddingService = embService
	s.embeddingStore = embStore

	// Register observer for automatic embedding of attestations with rich text
	observer := &EmbeddingObserver{
		embeddingService: embService,
		embeddingStore:   embStore,
		richStore:        storage.NewBoundedStore(s.db, s.logger.Named("auto-embed")),
		logger:           s.logger.Named("auto-embed"),
		clusterThreshold: float32(appcfg.GetFloat64("embeddings.cluster_threshold")),
		projectFunc:      s.projectToCanvas,
	}
	storage.RegisterObserver(observer)
	s.embeddingClusterInvalidator = observer.InvalidateClusterCache

	s.logger.Infow("Embedding service initialized",
		"path", modelPath,
		"name", modelName)
}

// hasRustEmbeddings returns true if compiled with rustembeddings build tag
func hasRustEmbeddings() bool {
	// This function will be overridden by the build tag
	return true
}

// EmbeddingObserver automatically embeds attestations that contain rich text.
// Implements storage.AttestationObserver — called asynchronously in a goroutine
// by notifyObservers, so errors are logged but don't block attestation creation.
// Only attestations with non-empty rich string fields (message, description, etc.)
// trigger embedding; structural-only attestations are silently skipped.
type EmbeddingObserver struct {
	embeddingService interface {
		GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
		DeserializeEmbedding(data []byte) ([]float32, error)
		ComputeSimilarity(a, b []float32) (float32, error)
		GetModelInfo() (*embeddings.ModelInfo, error)
	}
	embeddingStore   *storage.EmbeddingStore
	richStore        *storage.BoundedStore // Reused across calls for 5-min rich field cache
	logger           *zap.SugaredLogger
	clusterMu        sync.RWMutex
	clusterCache     []storage.ClusterCentroid // loaded once, refreshed on re-cluster
	clusterThreshold float32                   // minimum similarity for cluster assignment
	projectFunc      func(embeddingID string, embedding []float32)
}

// InvalidateClusterCache clears cached centroids so the next prediction reloads from DB.
func (o *EmbeddingObserver) InvalidateClusterCache() {
	o.clusterMu.Lock()
	o.clusterCache = nil
	o.clusterMu.Unlock()
}

// OnAttestationCreated selectively embeds attestations with rich text content.
func (o *EmbeddingObserver) OnAttestationCreated(as *types.As) {
	text := o.extractRichText(as)
	if text == "" {
		return
	}

	// Check if already embedded
	existing, err := o.embeddingStore.GetBySource("attestation", as.ID)
	if err != nil {
		o.logger.Warnw("Failed to check existing embedding",
			"error", errors.Wrapf(err, "attestation %s", as.ID))
		return
	}
	if existing != nil {
		return
	}

	// Generate embedding via Rust FFI (~80ms)
	result, err := o.embeddingService.GenerateEmbedding(text)
	if err != nil {
		o.logger.Warnw("Failed to generate embedding",
			"error", errors.Wrapf(err, "attestation %s (%d chars)", as.ID, len(text)))
		return
	}

	blob, err := o.embeddingService.SerializeEmbedding(result.Embedding)
	if err != nil {
		o.logger.Warnw("Failed to serialize embedding",
			"error", errors.Wrapf(err, "attestation %s (%d dimensions)", as.ID, len(result.Embedding)))
		return
	}

	modelInfo, err := o.embeddingService.GetModelInfo()
	if err != nil {
		o.logger.Warnw("Failed to get model info",
			"error", errors.Wrapf(err, "attestation %s", as.ID))
		return
	}

	model := &storage.EmbeddingModel{
		SourceType: "attestation",
		SourceID:   as.ID,
		Text:       text,
		Embedding:  blob,
		Model:      modelInfo.Name,
		Dimensions: modelInfo.Dimensions,
	}
	if err := o.embeddingStore.Save(model); err != nil {
		o.logger.Warnw("Failed to save embedding",
			"error", errors.Wrapf(err, "attestation %s", as.ID))
		return
	}

	o.logger.Infow("Auto-embedded attestation",
		"attestation_id", as.ID,
		"text_length", len(text),
		"inference_ms", result.InferenceMS)

	// Predict cluster assignment for the new embedding
	o.predictCluster(model.ID, as.ID, result.Embedding)

	// Project to 2D canvas if reduce plugin is available
	if o.projectFunc != nil {
		o.projectFunc(model.ID, result.Embedding)
	}
}

// predictCluster assigns the embedding to the nearest cluster centroid.
func (o *EmbeddingObserver) predictCluster(embeddingID, attestationID string, embedding []float32) {
	// Lazy-load centroids from DB
	o.clusterMu.RLock()
	centroids := o.clusterCache
	o.clusterMu.RUnlock()

	if centroids == nil {
		loaded, err := o.embeddingStore.GetAllClusterCentroids()
		if err != nil {
			o.logger.Warnw("Failed to load cluster centroids",
				"error", errors.Wrapf(err, "attestation %s", attestationID))
			return
		}
		if len(loaded) == 0 {
			return // no clusters yet
		}
		o.clusterMu.Lock()
		o.clusterCache = loaded
		o.clusterMu.Unlock()
		centroids = loaded
	}

	clusterID, prob, err := o.embeddingStore.PredictCluster(
		embedding,
		centroids,
		o.embeddingService.DeserializeEmbedding,
		o.embeddingService.ComputeSimilarity,
		o.clusterThreshold,
	)
	if err != nil {
		o.logger.Warnw("Failed to predict cluster",
			"error", errors.Wrapf(err, "attestation %s", attestationID))
		return
	}

	if clusterID == storage.ClusterNoise {
		return // below threshold, stays as noise
	}

	err = o.embeddingStore.UpdateClusterAssignments([]storage.ClusterAssignment{{
		ID:          embeddingID,
		ClusterID:   clusterID,
		Probability: prob,
	}})
	if err != nil {
		o.logger.Warnw("Failed to save predicted cluster assignment",
			"error", errors.Wrapf(err, "attestation %s embedding %s cluster %d", attestationID, embeddingID, clusterID))
		return
	}

	o.logger.Infow("Predicted cluster for new embedding",
		"attestation_id", attestationID,
		"embedding_id", embeddingID,
		"cluster_id", clusterID,
		"similarity", prob)
}

// extractRichText returns the concatenated rich text fields from an attestation's
// attributes. Returns empty string if no rich text is found — this is the
// selective gate that prevents embedding structural-only attestations.
func (o *EmbeddingObserver) extractRichText(as *types.As) string {
	if as.Attributes == nil || len(as.Attributes) == 0 {
		return ""
	}

	richFields := o.richStore.GetDiscoveredRichFields()
	if len(richFields) == 0 {
		return ""
	}

	return extractRichTextFromAttributes(as.Attributes, richFields)
}

// extractRichTextFromAttributes extracts text from the named rich fields in an
// attestation's attribute map. Shared by EmbeddingObserver and batch handler.
func extractRichTextFromAttributes(attrs map[string]interface{}, richFields []string) string {
	var parts []string
	for _, field := range richFields {
		value, exists := attrs[field]
		if !exists {
			continue
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok && str != "" {
					parts = append(parts, str)
				}
			}
		}
	}

	return strings.Join(parts, " ")
}

// callReducePlugin sends an HTTP request to the reduce plugin via gRPC.
// Returns the response body or an error.
func (s *QNTXServer) callReducePlugin(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if s.pluginRegistry == nil {
		return nil, errors.New("plugin registry not available")
	}
	p, ok := s.pluginRegistry.Get("reduce")
	if !ok {
		return nil, errors.New("reduce plugin not registered")
	}
	proxy, ok := p.(*grpcplugin.ExternalDomainProxy)
	if !ok {
		return nil, errors.New("reduce plugin is not a gRPC plugin")
	}

	resp, err := proxy.Client().HandleHTTP(ctx, &protocol.HTTPRequest{
		Method: method,
		Path:   path,
		Body:   body,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "reduce plugin %s %s gRPC call failed", method, path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Newf("reduce plugin %s %s returned status %d: %s",
			method, path, resp.StatusCode, string(resp.Body))
	}
	return resp.Body, nil
}

// HandleEmbeddingProject runs UMAP on all embeddings and stores 2D projections.
// POST /api/embeddings/project
func (s *QNTXServer) HandleEmbeddingProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingService == nil || s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	startTime := time.Now()

	// Read all embedding vectors
	ids, blobs, err := s.embeddingStore.GetAllEmbeddingVectors()
	if err != nil {
		s.logger.Errorw("Failed to read embedding vectors for projection", "error", err)
		http.Error(w, "Failed to read embeddings", http.StatusInternalServerError)
		return
	}

	if len(ids) < 2 {
		http.Error(w, fmt.Sprintf("Need at least 2 embeddings to project, have %d", len(ids)), http.StatusBadRequest)
		return
	}

	// Deserialize blobs into float32 arrays
	allEmbeddings := make([][]float32, 0, len(blobs))
	for i, blob := range blobs {
		vec, err := s.embeddingService.DeserializeEmbedding(blob)
		if err != nil {
			s.logger.Errorw("Failed to deserialize embedding for projection",
				"embedding_id", ids[i], "error", err)
			http.Error(w, fmt.Sprintf("Failed to deserialize embedding %s", ids[i]), http.StatusInternalServerError)
			return
		}
		allEmbeddings = append(allEmbeddings, vec)
	}

	// Call reduce plugin /fit
	fitReq, err := json.Marshal(map[string]interface{}{
		"embeddings": allEmbeddings,
	})
	if err != nil {
		http.Error(w, "Failed to marshal fit request", http.StatusInternalServerError)
		return
	}

	fitResp, err := s.callReducePlugin(r.Context(), "POST", "/fit", fitReq)
	if err != nil {
		s.logger.Errorw("Reduce plugin /fit failed", "n_points", len(ids), "error", err)
		http.Error(w, fmt.Sprintf("UMAP fit failed: %s", err), http.StatusInternalServerError)
		return
	}

	// Parse projections
	var fitResult struct {
		Projections [][]float64 `json:"projections"`
		NPoints     int         `json:"n_points"`
		FitMS       int64       `json:"fit_ms"`
	}
	if err := json.Unmarshal(fitResp, &fitResult); err != nil {
		s.logger.Errorw("Failed to parse reduce plugin response", "error", err)
		http.Error(w, "Failed to parse UMAP response", http.StatusInternalServerError)
		return
	}

	if len(fitResult.Projections) != len(ids) {
		http.Error(w, fmt.Sprintf("Projection count mismatch: got %d, expected %d",
			len(fitResult.Projections), len(ids)), http.StatusInternalServerError)
		return
	}

	// Write projections to DB
	assignments := make([]storage.ProjectionAssignment, len(ids))
	for i, id := range ids {
		assignments[i] = storage.ProjectionAssignment{
			ID:          id,
			ProjectionX: fitResult.Projections[i][0],
			ProjectionY: fitResult.Projections[i][1],
		}
	}

	if err := s.embeddingStore.UpdateProjections(assignments); err != nil {
		s.logger.Errorw("Failed to save projections", "count", len(assignments), "error", err)
		http.Error(w, "Failed to save projections", http.StatusInternalServerError)
		return
	}

	totalMS := time.Since(startTime).Milliseconds()

	s.logger.Infow("UMAP projection complete",
		"n_points", len(ids),
		"fit_ms", fitResult.FitMS,
		"total_ms", totalMS)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"n_points": len(ids),
		"fit_ms":   fitResult.FitMS,
		"total_ms": totalMS,
	})
}

// HandleEmbeddingProjections serves 2D projections for frontend visualization.
// GET /api/embeddings/projections
func (s *QNTXServer) HandleEmbeddingProjections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	projections, err := s.embeddingStore.GetAllProjections()
	if err != nil {
		s.logger.Errorw("Failed to get projections", "error", err)
		http.Error(w, "Failed to retrieve projections", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projections)
}

// projectToCanvas projects a single embedding to 2D via the reduce plugin's /transform.
// Silently returns if the plugin is not available or not fitted.
func (s *QNTXServer) projectToCanvas(embeddingID string, embedding []float32) {
	if s.pluginRegistry == nil {
		return
	}
	if _, ok := s.pluginRegistry.Get("reduce"); !ok {
		return
	}

	reqBody, err := json.Marshal(map[string]interface{}{
		"embeddings": [][]float32{embedding},
	})
	if err != nil {
		s.logger.Warnw("Failed to marshal transform request", "embedding_id", embeddingID, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := s.callReducePlugin(ctx, "POST", "/transform", reqBody)
	if err != nil {
		// Silently skip — plugin may not be fitted yet
		s.logger.Debugw("Transform skipped (plugin not fitted or unavailable)",
			"embedding_id", embeddingID, "error", err)
		return
	}

	var result struct {
		Projections [][]float64 `json:"projections"`
	}
	if err := json.Unmarshal(resp, &result); err != nil || len(result.Projections) == 0 {
		s.logger.Warnw("Failed to parse transform response",
			"embedding_id", embeddingID, "error", err)
		return
	}

	err = s.embeddingStore.UpdateProjections([]storage.ProjectionAssignment{{
		ID:          embeddingID,
		ProjectionX: result.Projections[0][0],
		ProjectionY: result.Projections[0][1],
	}})
	if err != nil {
		s.logger.Warnw("Failed to save projection for new embedding",
			"embedding_id", embeddingID, "error", err)
		return
	}

	s.logger.Debugw("Auto-projected new embedding",
		"embedding_id", embeddingID,
		"x", result.Projections[0][0],
		"y", result.Projections[0][1])
}

// --- Pulse HDBSCAN recluster handler ---

const ReclusterHandlerName = "embeddings.recluster"

// ReclusterHandler runs HDBSCAN re-clustering as a Pulse scheduled job
type ReclusterHandler struct {
	store          *storage.EmbeddingStore
	svc            EmbeddingServiceForClustering
	invalidator    func()
	minClusterSize int
	logger         *zap.SugaredLogger
}

func (h *ReclusterHandler) Name() string { return ReclusterHandlerName }

func (h *ReclusterHandler) Execute(ctx context.Context, job *async.Job) error {
	_, err := RunHDBSCANClustering(h.store, h.svc, h.invalidator, h.minClusterSize, h.logger)
	return err
}

// setupEmbeddingReclusterSchedule registers the recluster handler and auto-creates
// a Pulse schedule if embeddings.recluster_interval_seconds > 0.
func (s *QNTXServer) setupEmbeddingReclusterSchedule(cfg *appcfg.Config) {
	if s.embeddingService == nil || s.embeddingStore == nil {
		return
	}

	handler := &ReclusterHandler{
		store:          s.embeddingStore,
		svc:            s.embeddingService,
		invalidator:    s.embeddingClusterInvalidator,
		minClusterSize: cfg.Embeddings.MinClusterSize,
		logger:         s.logger.Named("recluster"),
	}
	if handler.minClusterSize <= 0 {
		handler.minClusterSize = 5
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered HDBSCAN recluster handler")

	interval := cfg.Embeddings.ReclusterIntervalSeconds
	if interval <= 0 {
		return
	}

	// Check for existing schedule to avoid duplicates on restart
	schedStore := schedule.NewStore(s.db)
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for recluster idempotency check",
			"handler_name", ReclusterHandlerName,
			"error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == ReclusterHandlerName && j.State == schedule.StateActive {
			// Update interval if it changed
			if j.IntervalSeconds != interval {
				if err := schedStore.UpdateJobInterval(j.ID, interval); err != nil {
					s.logger.Errorw("Failed to update recluster schedule interval",
						"job_id", j.ID,
						"error", err)
				} else {
					s.logger.Infow("Updated HDBSCAN recluster schedule interval",
						"job_id", j.ID,
						"interval_seconds", interval)
				}
			}
			return
		}
	}

	now := time.Now()
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_recluster_%d", now.Unix()),
		HandlerName:     ReclusterHandlerName,
		IntervalSeconds: interval,
		State:           schedule.StateActive,
		NextRunAt:       &now,
	}
	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create HDBSCAN recluster schedule",
			"interval_seconds", interval,
			"error", err)
		return
	}
	s.logger.Infow("Auto-created HDBSCAN recluster schedule",
		"job_id", job.ID,
		"interval_seconds", interval)
}
