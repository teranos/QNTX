package server

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/ai/provider"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/logger"
)

// PromptPreviewRequest represents a request to preview prompt execution with X-sampling
type PromptPreviewRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`                    // Prompt template with {{field}} placeholders
	SystemPrompt string `json:"system_prompt,omitempty"`      // Optional system instruction for the LLM
	SampleSize   int    `json:"sample_size,omitempty"`        // X value: number of samples to test (default: 1)
	Provider     string `json:"provider,omitempty"`           // "openrouter" or "local"
	Model        string `json:"model,omitempty"`               // Model override
	PromptID     string `json:"prompt_id,omitempty"`          // Optional prompt ID for tracking
	PromptVersion int   `json:"prompt_version,omitempty"`     // Optional prompt version for comparison
}

// PreviewSample represents a single sample execution result
type PreviewSample struct {
	Attestation      map[string]interface{} `json:"attestation"`       // The sampled attestation
	InterpolatedPrompt string               `json:"interpolated_prompt"` // Prompt after template interpolation
	Response         string                 `json:"response"`           // LLM response
	PromptTokens     int                    `json:"prompt_tokens,omitempty"`
	CompletionTokens int                    `json:"completion_tokens,omitempty"`
	TotalTokens      int                    `json:"total_tokens,omitempty"`
	Error            string                 `json:"error,omitempty"`    // Per-sample error if any
}

// PromptPreviewResponse represents the preview response with X samples
type PromptPreviewResponse struct {
	TotalAttestations int             `json:"total_attestations"`   // Total matching attestations from ax query
	SampleSize        int             `json:"sample_size"`          // X value used for sampling
	Samples           []PreviewSample `json:"samples"`              // X sample execution results
	Error             string          `json:"error,omitempty"`      // Global error if any
}

// PromptExecuteRequest represents a request to execute a prompt
type PromptExecuteRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Provider     string `json:"provider,omitempty"` // "openrouter" or "local"
	Model        string `json:"model,omitempty"`
}

// Result represents the output of a prompt execution
type Result struct {
	// SourceAttestationID is the ID of the attestation that was processed
	SourceAttestationID string `json:"source_attestation_id"`

	// Prompt is the interpolated prompt that was sent to the LLM
	Prompt string `json:"prompt"`

	// Response is the LLM's response
	Response string `json:"response"`

	// ResultAttestationID is the ID of the created result attestation
	ResultAttestationID string `json:"result_attestation_id,omitempty"`

	// Token usage tracking
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

// PromptExecuteResponse represents the execution response
type PromptExecuteResponse struct {
	Results          []Result `json:"results"`
	AttestationCount int      `json:"attestation_count"`
	Error            string   `json:"error,omitempty"`
}

// HandlePromptPreview handles POST /api/prompt/preview
// Samples X attestations, executes prompt against them, and returns results for comparison
func (s *QNTXServer) HandlePromptPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	logger.AddAxSymbol(s.logger).Infow("Prompt preview request with X-sampling")

	var req PromptPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWrappedError(w, s.logger, errors.Wrap(err, "failed to decode request"),
			"Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if strings.TrimSpace(req.AxQuery) == "" {
		writeError(w, http.StatusBadRequest, "ax_query is required")
		return
	}
	if strings.TrimSpace(req.Template) == "" {
		writeError(w, http.StatusBadRequest, "template is required")
		return
	}

	// Default sample size to 1 if not specified
	if req.SampleSize <= 0 {
		req.SampleSize = 1
	}

	// Parse the ax query - support both natural language and simple "TEST-TASK-1" format
	var filter *types.AxFilter
	args := strings.Fields(req.AxQuery)

	// Try parsing as natural language ax command
	parsedFilter, err := parser.ParseAxCommandWithContext(args, 0, parser.ErrorContextPlain)
	if err != nil {
		// Check if it's just a warning (best-effort parsing)
		if _, isWarning := err.(*parser.ParseWarning); !isWarning {
			// If it fails, try treating it as a simple subject query
			filter = &types.AxFilter{
				Subjects: []string{req.AxQuery},
				Limit:    100,
			}
		} else {
			filter = parsedFilter
		}
	} else {
		filter = parsedFilter
	}

	// Execute the query using storage executor
	executor := storage.NewExecutor(s.db)
	result, err := executor.ExecuteAsk(r.Context(), *filter)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Failed to execute ax query", http.StatusInternalServerError)
		return
	}

	totalAttestations := len(result.Attestations)
	if totalAttestations == 0 {
		// No attestations to preview
		resp := PromptPreviewResponse{
			TotalAttestations: 0,
			SampleSize:        req.SampleSize,
			Samples:           []PreviewSample{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	// Parse frontmatter and template
	doc, err := prompt.ParseFrontmatter(req.Template)
	if err != nil {
		wrappedErr := errors.Wrap(err, "failed to parse frontmatter")
		logger.AddAxSymbol(s.logger).Errorw("Frontmatter parsing failed",
			"error", wrappedErr,
			"template_length", len(req.Template),
		)
		writeWrappedError(w, s.logger, wrappedErr,
			"Failed to parse frontmatter", http.StatusBadRequest)
		return
	}

	// Parse template body (after frontmatter)
	tmpl, err := prompt.Parse(doc.Body)
	if err != nil {
		wrappedErr := errors.Wrap(err, "failed to parse prompt template")
		logger.AddAxSymbol(s.logger).Errorw("Template parsing failed",
			"error", wrappedErr,
			"template_length", len(doc.Body),
		)
		writeWrappedError(w, s.logger, wrappedErr,
			"Failed to parse prompt template", http.StatusBadRequest)
		return
	}

	// X-sampling: randomly sample attestations
	actualSampleSize := req.SampleSize
	if actualSampleSize > totalAttestations {
		actualSampleSize = totalAttestations
	}

	sampledAttestations := sampleAttestations(result.Attestations, actualSampleSize)

	// Create AI client
	client := s.createPromptAIClientForPreview(req, doc)

	// Process each sampled attestation
	samples := make([]PreviewSample, len(sampledAttestations))
	for i, as := range sampledAttestations {
		// Convert attestation to map for response
		attestationMap := map[string]interface{}{
			"id":         as.ID,
			"subjects":   as.Subjects,
			"predicates": as.Predicates,
			"contexts":   as.Contexts,
			"actors":     as.Actors,
			"timestamp":  as.Timestamp,
			"source":     as.Source,
			"attributes": as.Attributes,
		}

		// Interpolate template
		interpolatedPrompt, err := tmpl.Execute(&as)
		if err != nil {
			samples[i] = PreviewSample{
				Attestation:        attestationMap,
				InterpolatedPrompt: "",
				Error:              fmt.Sprintf("Failed to interpolate template: %v", err),
			}
			continue
		}

		// Call LLM
		chatReq := openrouter.ChatRequest{
			SystemPrompt: req.SystemPrompt,
			UserPrompt:   interpolatedPrompt,
		}

		// Set model if specified
		if req.Model != "" {
			chatReq.Model = &req.Model
		} else if doc.Metadata.Model != "" {
			chatReq.Model = &doc.Metadata.Model
		}

		// Set temperature if specified in frontmatter
		if doc.Metadata.Temperature != nil {
			chatReq.Temperature = doc.Metadata.Temperature
		}

		// Set max tokens if specified in frontmatter
		if doc.Metadata.MaxTokens != nil {
			chatReq.MaxTokens = doc.Metadata.MaxTokens
		}

		// Execute prompt
		resp, err := client.Chat(r.Context(), chatReq)
		if err != nil {
			samples[i] = PreviewSample{
				Attestation:        attestationMap,
				InterpolatedPrompt: interpolatedPrompt,
				Error:              fmt.Sprintf("LLM call failed: %v", err),
			}
			continue
		}

		// Successful sample
		samples[i] = PreviewSample{
			Attestation:        attestationMap,
			InterpolatedPrompt: interpolatedPrompt,
			Response:           resp.Content,
			PromptTokens:       resp.Usage.PromptTokens,
			CompletionTokens:   resp.Usage.CompletionTokens,
			TotalTokens:        resp.Usage.TotalTokens,
		}
	}

	// Build response
	response := PromptPreviewResponse{
		TotalAttestations: totalAttestations,
		SampleSize:        actualSampleSize,
		Samples:           samples,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandlePromptExecute handles POST /api/prompt/execute
// Executes a prompt template against attestations and returns LLM responses
func (s *QNTXServer) HandlePromptExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	logger.AddAxSymbol(s.logger).Infow("Prompt execute request")

	var req PromptExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWrappedError(w, s.logger, errors.Wrap(err, "failed to decode request"),
			"Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validation
	if strings.TrimSpace(req.AxQuery) == "" {
		writeError(w, http.StatusBadRequest, "ax_query is required")
		return
	}
	if strings.TrimSpace(req.Template) == "" {
		writeError(w, http.StatusBadRequest, "template is required")
		return
	}

	// Validate template syntax
	if err := prompt.ValidateTemplate(req.Template); err != nil {
		writeWrappedError(w, s.logger, errors.Wrap(err, "template validation failed"),
			"Invalid template", http.StatusBadRequest)
		return
	}

	// Parse the ax query
	args := strings.Fields(req.AxQuery)
	filter, err := parser.ParseAxCommandWithContext(args, 0, parser.ErrorContextPlain)
	if err != nil {
		if _, isWarning := err.(*parser.ParseWarning); !isWarning {
			writeWrappedError(w, s.logger, errors.Wrap(err, "failed to parse ax query"),
				"Invalid ax query", http.StatusBadRequest)
			return
		}
	}

	// Create query store and alias resolver
	queryStore := storage.NewSQLQueryStore(s.db)
	aliasStore := storage.NewAliasStore(s.db)
	aliasResolver := alias.NewResolver(aliasStore)

	// Create AI client based on request or config
	client := s.createPromptAIClient(req.Provider, req.Model)

	// Execute the prompt using one-shot mode
	promptResults, err := prompt.ExecuteOneShot(
		r.Context(),
		queryStore,
		aliasResolver,
		client,
		*filter,
		req.Template,
		req.SystemPrompt,
	)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Prompt execution failed", http.StatusInternalServerError)
		return
	}

	// Convert prompt.Result to server.Result
	results := make([]Result, len(promptResults))
	for i, pr := range promptResults {
		results[i] = Result{
			SourceAttestationID: pr.SourceAttestationID,
			Prompt:              pr.Prompt,
			Response:            pr.Response,
			ResultAttestationID: pr.ResultAttestationID,
			PromptTokens:        pr.Usage.PromptTokens,
			CompletionTokens:    pr.Usage.CompletionTokens,
			TotalTokens:         pr.Usage.TotalTokens,
		}
	}

	resp := PromptExecuteResponse{
		Results:          results,
		AttestationCount: len(results),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// createPromptAIClient creates an AI client for prompt execution
func (s *QNTXServer) createPromptAIClient(providerName, model string) provider.AIClient {
	// Read config values using am package
	localEnabled := appcfg.GetBool("local_inference.enabled")
	localBaseURL := appcfg.GetString("local_inference.base_url")
	localModel := appcfg.GetString("local_inference.model")
	localTimeout := appcfg.GetInt("local_inference.timeout_seconds")
	openrouterAPIKey := appcfg.GetString("openrouter.api_key")
	openrouterModel := appcfg.GetString("openrouter.model")

	// Determine provider
	useLocal := false
	if providerName == "local" {
		useLocal = true
	} else if providerName == "" && localEnabled {
		useLocal = true
	}

	if useLocal {
		effectiveModel := model
		if effectiveModel == "" {
			effectiveModel = localModel
		}
		if effectiveModel == "" {
			effectiveModel = "llama3.2:3b"
		}

		return provider.NewLocalClient(provider.LocalClientConfig{
			BaseURL:        localBaseURL,
			Model:          effectiveModel,
			TimeoutSeconds: localTimeout,
			DB:             s.db,
			OperationType:  "prompt-execute",
		})
	}

	// OpenRouter
	effectiveModel := model
	if effectiveModel == "" {
		effectiveModel = openrouterModel
	}
	if effectiveModel == "" {
		effectiveModel = "openai/gpt-4o-mini"
	}

	return openrouter.NewClient(openrouter.Config{
		APIKey:        openrouterAPIKey,
		Model:         effectiveModel,
		DB:            s.db,
		OperationType: "prompt-execute",
	})
}

// PromptSaveRequest represents a request to save a prompt
type PromptSaveRequest struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	AxPattern    string `json:"ax_pattern,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
}

// HandlePromptList handles GET /api/prompt/list
// Returns all stored prompts
func (s *QNTXServer) HandlePromptList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	logger.AddAxSymbol(s.logger).Infow("Prompt list request")

	store := prompt.NewPromptStore(s.db)
	prompts, err := store.ListPrompts(r.Context(), 100)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Failed to list prompts", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"prompts": prompts,
		"count":   len(prompts),
	})
}

// HandlePromptGet handles GET /api/prompt/{id}
// Returns a specific prompt by ID
func (s *QNTXServer) HandlePromptGet(w http.ResponseWriter, r *http.Request, promptID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	store := prompt.NewPromptStore(s.db)
	p, err := store.GetPromptByID(r.Context(), promptID)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Failed to get prompt", http.StatusInternalServerError)
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "Prompt not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// HandlePromptVersions handles GET /api/prompt/{name}/versions
// Returns version history for a prompt
func (s *QNTXServer) HandlePromptVersions(w http.ResponseWriter, r *http.Request, promptName string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	store := prompt.NewPromptStore(s.db)
	versions, err := store.GetPromptVersions(r.Context(), promptName, 16)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Failed to get prompt versions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"versions": versions,
		"count":    len(versions),
	})
}

// HandlePromptSave handles POST /api/prompt/save
// Saves a new prompt or creates a new version
func (s *QNTXServer) HandlePromptSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	logger.AddAxSymbol(s.logger).Infow("Prompt save request")

	var req PromptSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeWrappedError(w, s.logger, errors.Wrap(err, "failed to decode request"),
			"Invalid JSON", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Template) == "" {
		writeError(w, http.StatusBadRequest, "template is required")
		return
	}

	store := prompt.NewPromptStore(s.db)
	storedPrompt := &prompt.StoredPrompt{
		Name:         req.Name,
		Template:     req.Template,
		SystemPrompt: req.SystemPrompt,
		AxPattern:    req.AxPattern,
		Provider:     req.Provider,
		Model:        req.Model,
	}

	// Default actor - could be from auth later
	actor := "user"

	saved, err := store.SavePrompt(r.Context(), storedPrompt, actor)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Failed to save prompt", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(saved)
}

// sampleAttestations randomly samples n attestations from the provided list
func sampleAttestations(attestations []types.As, n int) []types.As {
	if n >= len(attestations) {
		// Return all attestations if sample size >= total
		return attestations
	}

	// Create a copy and shuffle using Fisher-Yates
	sampled := make([]types.As, len(attestations))
	copy(sampled, attestations)

	// Shuffle the first n elements
	for i := 0; i < n; i++ {
		j := i + rand.Intn(len(sampled)-i)
		sampled[i], sampled[j] = sampled[j], sampled[i]
	}

	// Return only the first n elements
	return sampled[:n]
}

// createPromptAIClientForPreview creates an AI client based on the request and frontmatter configuration
func (s *QNTXServer) createPromptAIClientForPreview(req PromptPreviewRequest, doc *prompt.PromptDocument) provider.AIClient {
	// Read config values using am package
	localEnabled := appcfg.GetBool("local_inference.enabled")
	localBaseURL := appcfg.GetString("local_inference.base_url")
	localModel := appcfg.GetString("local_inference.model")
	localTimeout := appcfg.GetInt("local_inference.timeout_seconds")
	openRouterAPIKey := appcfg.GetString("openrouter.api_key")
	openRouterModel := appcfg.GetString("openrouter.model")

	// Determine provider (request > config default)
	providerName := req.Provider

	// Determine model (request > frontmatter > config default)
	model := req.Model
	if model == "" && doc.Metadata.Model != "" {
		model = doc.Metadata.Model
	}

	// Use provider factory to create the appropriate client
	if providerName == "local" || (providerName == "" && localEnabled) {
		if model == "" {
			model = localModel
		}
		if model == "" {
			model = "llama3.2:3b" // default fallback
		}
		return provider.NewLocalClient(provider.LocalClientConfig{
			BaseURL:        localBaseURL,
			Model:          model,
			TimeoutSeconds: localTimeout,
			DB:             s.db,
			OperationType:  "prompt-preview",
		})
	}

	// Default to OpenRouter
	if model == "" {
		model = openRouterModel
	}
	if model == "" {
		model = "openai/gpt-4o-mini" // default fallback
	}
	return openrouter.NewClient(openrouter.Config{
		APIKey:        openRouterAPIKey,
		Model:         model,
		DB:            s.db,
		OperationType: "prompt-preview",
	})
}

// HandlePrompt routes prompt-related requests
func (s *QNTXServer) HandlePrompt(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/prompt")

	switch {
	case path == "/preview":
		s.HandlePromptPreview(w, r)
	case path == "/execute":
		s.HandlePromptExecute(w, r)
	case path == "/list":
		s.HandlePromptList(w, r)
	case path == "/save":
		s.HandlePromptSave(w, r)
	case strings.HasSuffix(path, "/versions"):
		// /api/prompt/{name}/versions
		name := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/versions")
		s.HandlePromptVersions(w, r, name)
	case strings.HasPrefix(path, "/"):
		// /api/prompt/{id}
		promptID := strings.TrimPrefix(path, "/")
		if promptID != "" {
			s.HandlePromptGet(w, r, promptID)
			return
		}
		fallthrough
	default:
		writeError(w, http.StatusNotFound, "Unknown prompt endpoint")
	}
}
