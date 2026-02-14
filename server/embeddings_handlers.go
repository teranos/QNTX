//go:build cgo && rustembeddings

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
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
	searchResults, err := s.embeddingStore.SemanticSearch(queryBlob, limit, threshold)
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
	// Rich fields (message, description, etc.) produce better embeddings than
	// raw structural identifiers (predicates/subjects/contexts).
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

		// Build text for embedding: prefer rich text from attributes (same fields
		// the fuzzy search uses), fall back to structural fields if none found.
		textParts := []string{}
		if attestation.Attributes != nil && len(richFields) > 0 {
			for _, field := range richFields {
				if value, exists := attestation.Attributes[field]; exists {
					switch v := value.(type) {
					case string:
						if v != "" {
							textParts = append(textParts, v)
						}
					case []interface{}:
						for _, item := range v {
							if str, ok := item.(string); ok && str != "" {
								textParts = append(textParts, str)
							}
						}
					}
				}
			}
		}
		if len(textParts) == 0 {
			for _, pred := range attestation.Predicates {
				textParts = append(textParts, pred)
			}
			for _, subj := range attestation.Subjects {
				textParts = append(textParts, subj)
			}
			for _, ctx := range attestation.Contexts {
				textParts = append(textParts, ctx)
			}
		}

		text := strings.Join(textParts, " ")
		if text == "" {
			errorMessages = append(errorMessages, errors.Newf("attestation %s has no text content",
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
	}
	storage.RegisterObserver(observer)

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
// Implements storage.AttestationObserver — called asynchronously by notifyObservers.
// Only attestations with non-empty rich string fields (message, description, etc.)
// trigger embedding; structural-only attestations are silently skipped.
type EmbeddingObserver struct {
	embeddingService interface {
		GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
		GetModelInfo() (*embeddings.ModelInfo, error)
	}
	embeddingStore *storage.EmbeddingStore
	richStore      *storage.BoundedStore // Reused across calls for 5-min rich field cache
	logger         *zap.SugaredLogger
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

	var parts []string
	for _, field := range richFields {
		value, exists := as.Attributes[field]
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
