//go:build cgo && rustembeddings

package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
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
	inferenceMS := time.Now().Sub(startInference).Milliseconds()

	// Serialize embedding for sqlite-vec
	queryBlob, err := s.embeddingService.SerializeEmbedding(queryResult.Embedding)
	if err != nil {
		s.logger.Errorw("Failed to serialize query embedding",
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
			"error", err)
		http.Error(w, "Failed to perform search", http.StatusInternalServerError)
		return
	}
	searchMS := time.Now().Sub(startSearch).Milliseconds()

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
		// For attestation source type, fetch the attestation
		if result.SourceType == "attestation" {
			// Query attestation directly from database
			query := `
				SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
				FROM attestations WHERE id = ?
			`
			var attestation types.As
			var subjects, predicates, contexts, actors, attributes string
			var timestamp, createdAt string

			err := s.db.QueryRow(query, result.SourceID).Scan(
				&attestation.ID,
				&subjects,
				&predicates,
				&contexts,
				&actors,
				&timestamp,
				&attestation.Source,
				&attributes,
				&createdAt,
			)

			if err != nil {
				if err != sql.ErrNoRows {
					s.logger.Warnw("Failed to fetch attestation for search result",
						"attestation_id", result.SourceID,
						"error", err)
				}
				continue
			}

			// Parse JSON arrays
			if err := json.Unmarshal([]byte(subjects), &attestation.Subjects); err != nil {
				s.logger.Warnw("Failed to parse subjects",
					"attestation_id", result.SourceID,
					"error", err)
				continue
			}
			if err := json.Unmarshal([]byte(predicates), &attestation.Predicates); err != nil {
				s.logger.Warnw("Failed to parse predicates",
					"attestation_id", result.SourceID,
					"error", err)
				continue
			}
			if err := json.Unmarshal([]byte(contexts), &attestation.Contexts); err != nil {
				s.logger.Warnw("Failed to parse contexts",
					"attestation_id", result.SourceID,
					"error", err)
				continue
			}
			if err := json.Unmarshal([]byte(actors), &attestation.Actors); err != nil {
				s.logger.Warnw("Failed to parse actors",
					"attestation_id", result.SourceID,
					"error", err)
				continue
			}
			if attributes != "{}" && attributes != "" {
				if err := json.Unmarshal([]byte(attributes), &attestation.Attributes); err != nil {
					s.logger.Warnw("Failed to parse attributes",
						"attestation_id", result.SourceID,
						"error", err)
				}
			}

			// Parse timestamps
			attestation.Timestamp, _ = time.Parse(time.RFC3339, timestamp)
			attestation.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

			response.Results = append(response.Results, SemanticSearchResult{
				Attestation: &attestation,
				Similarity:  result.Similarity,
				Distance:    result.Distance,
			})
		}
	}

	response.Stats.TotalResults = len(response.Results)

	// Send response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode response",
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
		s.logger.Errorw("Failed to encode response",
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
		query := `
			SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
			FROM attestations WHERE id = ?
		`
		var attestation types.As
		var subjects, predicates, contexts, actors, attributes string
		var timestamp, createdAt string

		err = s.db.QueryRow(query, attestationID).Scan(
			&attestation.ID,
			&subjects,
			&predicates,
			&contexts,
			&actors,
			&timestamp,
			&attestation.Source,
			&attributes,
			&createdAt,
		)

		if err != nil {
			if err == sql.ErrNoRows {
				errorMessages = append(errorMessages, errors.Newf("attestation %s not found",
					attestationID).Error())
			} else {
				errorMessages = append(errorMessages, errors.Wrapf(err, "failed to fetch attestation %s",
					attestationID).Error())
			}
			failed++
			continue
		}

		// Parse JSON arrays
		if err := json.Unmarshal([]byte(subjects), &attestation.Subjects); err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to parse subjects for %s",
				attestationID).Error())
			failed++
			continue
		}
		if err := json.Unmarshal([]byte(predicates), &attestation.Predicates); err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to parse predicates for %s",
				attestationID).Error())
			failed++
			continue
		}
		if err := json.Unmarshal([]byte(contexts), &attestation.Contexts); err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to parse contexts for %s",
				attestationID).Error())
			failed++
			continue
		}
		if err := json.Unmarshal([]byte(actors), &attestation.Actors); err != nil {
			errorMessages = append(errorMessages, errors.Wrapf(err, "failed to parse actors for %s",
				attestationID).Error())
			failed++
			continue
		}

		// Build text for embedding from attestation fields
		textParts := []string{}

		// Add predicates
		for _, pred := range attestation.Predicates {
			textParts = append(textParts, pred)
		}

		// Add subjects
		for _, subj := range attestation.Subjects {
			textParts = append(textParts, subj)
		}

		// Add contexts
		for _, ctx := range attestation.Contexts {
			textParts = append(textParts, ctx)
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
			errorMessages = append(errorMessages, errors.Wrap(err, "failed to save embeddings to database").Error())
		}
	}

	// Send response
	response := EmbeddingBatchResponse{
		Processed: processed,
		Failed:    failed,
		TimeMS:    float64(time.Now().Sub(startTime).Milliseconds()),
	}

	if len(errorMessages) > 0 {
		response.Errors = errorMessages
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode response",
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

	// Initialize embedding service
	modelPath := "ats/embeddings/models/all-MiniLM-L6-v2/model.onnx"
	embService, err := embeddings.NewManagedEmbeddingService(modelPath)
	if err != nil {
		s.logger.Errorw("Failed to create embedding service",
			"error", err)
		return
	}

	// Initialize the service
	if err := embService.Initialize(); err != nil {
		s.logger.Errorw("Failed to initialize embedding service",
			"error", err)
		return
	}

	// Create embedding store
	embStore := storage.NewEmbeddingStore(s.db, s.logger.Desugar())

	// Store references
	s.embeddingService = embService
	s.embeddingStore = embStore

	s.logger.Infow("Embedding service initialized",
		"model_path", modelPath)
}

// hasRustEmbeddings returns true if compiled with rustembeddings build tag
func hasRustEmbeddings() bool {
	// This function will be overridden by the build tag
	return true
}
