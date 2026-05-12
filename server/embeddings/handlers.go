package embeddings

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// ── Semantic Search ─────────────────────────────────────────────────

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
func (h *Handler) HandleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Service == nil || h.Store == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

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

	startInference := time.Now()
	queryResult, err := h.Service.GenerateEmbedding(query, "")
	if err != nil {
		h.Logger.Errorw("Failed to generate query embedding", "query", query, "error", err)
		http.Error(w, "Failed to generate query embedding", http.StatusInternalServerError)
		return
	}
	inferenceMS := time.Since(startInference).Milliseconds()

	queryBlob, err := h.Service.SerializeEmbedding(queryResult.Embedding)
	if err != nil {
		h.Logger.Errorw("Failed to serialize query embedding",
			"query", query, "dimensions", len(queryResult.Embedding), "error", err)
		http.Error(w, "Failed to serialize embedding", http.StatusInternalServerError)
		return
	}

	startSearch := time.Now()
	searchResults, err := h.Store.SemanticSearch(queryBlob, limit, threshold, clusterID, "")
	if err != nil {
		h.Logger.Errorw("Failed to perform semantic search",
			"query", query, "limit", limit, "threshold", threshold, "error", err)
		http.Error(w, "Failed to perform search", http.StatusInternalServerError)
		return
	}
	searchMS := time.Since(startSearch).Milliseconds()

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
			attestation, err := h.getAttestationByID(result.SourceID)
			if err != nil {
				h.Logger.Warnw("Failed to fetch attestation for search result",
					"attestation_id", result.SourceID, "error", err)
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.Logger.Errorw("Failed to encode semantic search response",
			"result_count", len(response.Results), "error", err)
	}
}

// ── Generate ────────────────────────────────────────────────────────

// EmbeddingGenerateRequest represents an embedding generation request
type EmbeddingGenerateRequest struct {
	Text  string `json:"text"`
	Model string `json:"model,omitempty"`
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
func (h *Handler) HandleEmbeddingGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Service == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	var req EmbeddingGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "Text field is required", http.StatusBadRequest)
		return
	}

	result, err := h.Service.GenerateEmbedding(req.Text, req.Model)
	if err != nil {
		h.Logger.Errorw("Failed to generate embedding", "text_length", len(req.Text), "error", err)
		http.Error(w, "Failed to generate embedding", http.StatusInternalServerError)
		return
	}

	modelInfo, err := h.Service.GetModelInfo(req.Model)
	if err != nil {
		h.Logger.Errorw("Failed to get model info", "model", req.Model, "error", err)
		modelInfo = &ModelInfo{
			Name:       "unknown",
			Dimensions: len(result.Embedding),
		}
	}

	response := EmbeddingGenerateResponse{
		Embedding:   result.Embedding,
		Dimensions:  modelInfo.Dimensions,
		Model:       modelInfo.Name,
		Tokens:      result.Tokens,
		InferenceMS: result.InferenceMS,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.Logger.Errorw("Failed to encode embedding response",
			"dimensions", response.Dimensions, "error", err)
	}
}

// ── Batch ───────────────────────────────────────────────────────────

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
func (h *Handler) HandleEmbeddingBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Service == nil || h.Store == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

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

	modelInfo, err := h.Service.GetModelInfo("")
	if err != nil {
		h.Logger.Errorw("Failed to get model info", "error", err)
		http.Error(w, "Failed to get model info", http.StatusInternalServerError)
		return
	}

	embeddingModels := []*storage.EmbeddingModel{}

	richStore := storage.NewBoundedStore(h.DB, nil, h.Logger.Named("embeddings"))
	richFields := richStore.GetDiscoveredRichFields()

	for _, attestationID := range req.AttestationIDs {
		existing, err := h.Store.GetBySource("attestation", attestationID, "")
		if err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to check existing embedding for %s",
				attestationID).Error())
			failed++
			continue
		}

		if existing != nil {
			h.Logger.Debugw("Embedding already exists, skipping", "attestation_id", attestationID)
			processed++
			continue
		}

		attestation, err := h.getAttestationByID(attestationID)
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

		text := ExtractRichTextFromAttributes(attestation.Attributes, richFields)
		if text == "" {
			errorMessages = append(errorMessages, errors.Newf("attestation %s has no rich text content",
				attestationID).Error())
			failed++
			continue
		}

		result, err := h.Service.GenerateEmbedding(text, "")
		if err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to generate embedding for %s",
				attestationID).Error())
			failed++
			continue
		}

		embeddingBlob, err := h.Service.SerializeEmbedding(result.Embedding)
		if err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to serialize embedding for %s",
				attestationID).Error())
			failed++
			continue
		}

		embeddingModel := &storage.EmbeddingModel{
			ID:         "",
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

	if len(embeddingModels) > 0 {
		if err := h.Store.BatchSaveAttestationEmbeddings(embeddingModels); err != nil {
			h.Logger.Errorw("Failed to batch save embeddings", "count", len(embeddingModels), "error", err)
			failed += len(embeddingModels)
			processed -= len(embeddingModels)
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to save %d embeddings to database", len(embeddingModels)).Error())
		}
	}

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
		h.Logger.Errorw("Failed to encode batch response",
			"processed", response.Processed, "failed", response.Failed, "error", err)
	}
}

// ── By-Source ────────────────────────────────────────────────────────

// EmbeddingsBySourceRequest represents a request for embeddings by source IDs
type EmbeddingsBySourceRequest struct {
	SourceIDs []string `json:"source_ids"`
}

// EmbeddingsBySourceEntry represents a single embedding in the response
type EmbeddingsBySourceEntry struct {
	SourceID   string    `json:"source_id"`
	Vector     []float32 `json:"vector"`
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
}

// EmbeddingsBySourceResponse represents the response for embeddings by source
type EmbeddingsBySourceResponse struct {
	Embeddings []EmbeddingsBySourceEntry `json:"embeddings"`
}

// HandleEmbeddingsBySource returns embeddings for the given attestation source IDs.
// POST /api/embeddings/by-source
func (h *Handler) HandleEmbeddingsBySource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Store == nil || h.Service == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	var req EmbeddingsBySourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.SourceIDs) == 0 {
		http.Error(w, "source_ids field is required", http.StatusBadRequest)
		return
	}

	if len(req.SourceIDs) > 500 {
		http.Error(w, "Maximum 500 source_ids per request", http.StatusBadRequest)
		return
	}

	models, err := h.Store.GetBySourceIDs(req.SourceIDs)
	if err != nil {
		h.Logger.Errorw("Failed to get embeddings by source IDs", "count", len(req.SourceIDs), "error", err)
		http.Error(w, "Failed to retrieve embeddings", http.StatusInternalServerError)
		return
	}

	entries := make([]EmbeddingsBySourceEntry, 0, len(models))
	for _, m := range models {
		vec, err := h.Service.DeserializeEmbedding(m.Embedding)
		if err != nil {
			h.Logger.Warnw("Failed to deserialize embedding", "source_id", m.SourceID, "error", err)
			continue
		}
		entries = append(entries, EmbeddingsBySourceEntry{
			SourceID:   m.SourceID,
			Vector:     vec,
			Model:      m.Model,
			Dimensions: m.Dimensions,
		})
	}

	resp := EmbeddingsBySourceResponse{Embeddings: entries}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.Logger.Errorw("Failed to encode embeddings by-source response", "count", len(entries), "error", err)
	}
}

// ── Info ─────────────────────────────────────────────────────────────

// EmbeddingInfoResponse represents embedding service status
type EmbeddingInfoResponse struct {
	Available        bool                    `json:"available"`
	ModelName        string                  `json:"model_name"`
	Dimensions       int                     `json:"dimensions"`
	EmbeddingCount   int                     `json:"embedding_count"`
	AttestationCount int                     `json:"attestation_count"`
	UnembeddedIDs    []string                `json:"unembedded_ids,omitempty"`
	ClusterInfo      *storage.ClusterSummary `json:"cluster_info,omitempty"`
	HDBSCANConfig    *HDBSCANConfig          `json:"hdbscan_config,omitempty"`
}

// HDBSCANConfig exposes current clustering parameters to the frontend
type HDBSCANConfig struct {
	MinClusterSize        int     `json:"min_cluster_size"`
	ClusterThreshold      float64 `json:"cluster_threshold"`
	ClusterMatchThreshold float64 `json:"cluster_match_threshold"`
}

// HandleEmbeddingInfo returns embedding service status and counts (GET /api/embeddings/info)
func (h *Handler) HandleEmbeddingInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := EmbeddingInfoResponse{Available: h.Service != nil}

	if h.Service != nil {
		if info, err := h.Service.GetModelInfo(""); err == nil {
			resp.ModelName = info.Name
			resp.Dimensions = info.Dimensions
		}
	}

	var embCount, atsCount int
	if err := h.DB.QueryRow("SELECT COUNT(*) FROM embeddings").Scan(&embCount); err != nil {
		h.Logger.Errorw("Failed to count embeddings", "error", err)
		http.Error(w, "Failed to retrieve embedding count", http.StatusInternalServerError)
		return
	}
	type counter interface{ CountAttestations() (int, error) }
	if c, ok := h.ATSStore.(counter); ok {
		if cnt, err := c.CountAttestations(); err == nil {
			atsCount = cnt
		}
	}
	resp.EmbeddingCount = embCount
	resp.AttestationCount = atsCount

	rows, err := h.DB.Query(`
		SELECT a.id FROM attestations a
		LEFT JOIN embeddings e ON e.source_type = 'attestation' AND e.source_id = a.id
		WHERE e.id IS NULL
	`)
	if err != nil {
		h.Logger.Errorw("Failed to query unembedded attestations", "error", err)
		http.Error(w, "Failed to retrieve unembedded attestation list", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			h.Logger.Warnw("Failed to scan attestation ID", "error", err)
			continue
		}
		resp.UnembeddedIDs = append(resp.UnembeddedIDs, id)
	}
	if err := rows.Err(); err != nil {
		h.Logger.Warnw("Error iterating unembedded attestations", "error", err)
	}

	if h.Store != nil {
		if summary, err := h.Store.GetClusterSummary(); err == nil && summary.NClusters > 0 {
			resp.ClusterInfo = summary
		}
	}

	minCS := appcfg.GetInt("embeddings.min_cluster_size")
	if minCS <= 0 {
		minCS = 5
	}
	resp.HDBSCANConfig = &HDBSCANConfig{
		MinClusterSize:        minCS,
		ClusterThreshold:      appcfg.GetFloat64("embeddings.cluster_threshold"),
		ClusterMatchThreshold: appcfg.GetFloat64("embeddings.cluster_match_threshold"),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.Logger.Errorw("Failed to encode embeddings info response",
			"available", resp.Available, "embedding_count", resp.EmbeddingCount,
			"attestation_count", resp.AttestationCount, "error", err)
	}
}

// ── Projections ─────────────────────────────────────────────────────

// HandleEmbeddingProjections serves 2D projections for frontend visualization.
// GET /api/embeddings/projections
func (h *Handler) HandleEmbeddingProjections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.Store == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	methods, err := h.Store.GetAllProjectionMethods()
	if err != nil {
		h.Logger.Errorw("Failed to get projection methods", "error", err)
		http.Error(w, "Failed to retrieve projection methods", http.StatusInternalServerError)
		return
	}

	result := make(map[string][]storage.ProjectionWithCluster, len(methods))
	for _, method := range methods {
		projections, err := h.Store.GetProjectionsByMethod(method)
		if err != nil {
			h.Logger.Errorw("Failed to get projections", "method", method, "error", err)
			http.Error(w, "Failed to retrieve projections for "+method, http.StatusInternalServerError)
			return
		}
		result[method] = projections
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ── Utilities ───────────────────────────────────────────────────────

// ExtractRichTextFromAttributes extracts text from the named rich fields in an
// attestation's attribute map. Shared by EmbeddingObserver and batch handler.
func ExtractRichTextFromAttributes(attrs map[string]any, richFields []string) string {
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
		case []any:
			for _, item := range v {
				if str, ok := item.(string); ok && str != "" {
					parts = append(parts, str)
				}
			}
		}
	}

	return strings.Join(parts, " ")
}
