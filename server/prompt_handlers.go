package server

import (
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/ai/provider"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/logger"
	id "github.com/teranos/vanity-id"
)

const (
	// Default query limit when parsing ax queries without explicit limit
	defaultAxQueryLimit = 100
	// Default model for local inference when not configured
	defaultLocalModel = "llama3.2:3b"
	// Default model for OpenRouter when not configured
	defaultOpenRouterModel = "openai/gpt-4o-mini"
)

// PromptPreviewRequest represents a request to preview prompt execution with X-sampling
type PromptPreviewRequest struct {
	AxQuery       string `json:"ax_query"`
	Template      string `json:"template"`                 // Prompt template with {{field}} placeholders
	SystemPrompt  string `json:"system_prompt,omitempty"`  // Optional system instruction for the LLM
	SampleSize    int    `json:"sample_size,omitempty"`    // X value: number of samples to test (default: 1)
	Provider      string `json:"provider,omitempty"`       // "openrouter" or "local"
	Model         string `json:"model,omitempty"`          // Model override
	PromptID      string `json:"prompt_id,omitempty"`      // Optional prompt ID for tracking
	PromptVersion int    `json:"prompt_version,omitempty"` // Optional prompt version for comparison
}

// PreviewSample represents a single sample execution result
type PreviewSample struct {
	Attestation        map[string]interface{} `json:"attestation"`         // The sampled attestation
	InterpolatedPrompt string                 `json:"interpolated_prompt"` // Prompt after template interpolation
	Response           string                 `json:"response"`            // LLM response
	PromptTokens       int                    `json:"prompt_tokens,omitempty"`
	CompletionTokens   int                    `json:"completion_tokens,omitempty"`
	TotalTokens        int                    `json:"total_tokens,omitempty"`
	Error              string                 `json:"error,omitempty"` // Per-sample error if any
}

// PromptPreviewResponse represents the preview response with X samples
type PromptPreviewResponse struct {
	TotalAttestations int             `json:"total_attestations"` // Total matching attestations from ax query
	SampleSize        int             `json:"sample_size"`        // X value used for sampling
	Samples           []PreviewSample `json:"samples"`            // X sample execution results
	SuccessCount      int             `json:"success_count"`      // Number of successful samples
	FailureCount      int             `json:"failure_count"`      // Number of failed samples
	Error             string          `json:"error,omitempty"`    // Global error if any
}

// PromptExecuteRequest represents a request to execute a prompt
type PromptExecuteRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Provider     string `json:"provider,omitempty"` // "openrouter" or "local"
	Model        string `json:"model,omitempty"`
}

// PromptDirectRequest represents a request to execute a prompt without attestations
type PromptDirectRequest struct {
	Template            string    `json:"template"` // Prompt template with optional {{field}} placeholders
	SystemPrompt        string    `json:"system_prompt,omitempty"`
	Provider            string    `json:"provider,omitempty"` // "openrouter" or "local"
	Model               string    `json:"model,omitempty"`
	GlyphID             string    `json:"glyph_id,omitempty"`             // Glyph that initiated execution; used as actor for the result attestation
	UpstreamAttestation *types.As `json:"upstream_attestation,omitempty"` // Triggering attestation — enables {{field}} interpolation
	FileIDs             []string  `json:"file_ids,omitempty"`             // Attached document/image file IDs for multimodal prompts
}

// PromptDirectResponse represents the direct execution response
type PromptDirectResponse struct {
	Response         string `json:"response"`
	AttestationID    string `json:"attestation_id,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	TotalTokens      int    `json:"total_tokens,omitempty"`
	Error            string `json:"error,omitempty"`
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
	if err := readJSON(w, r, &req); err != nil {
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

	// Limit sample size to prevent excessive LLM costs
	// For very large X-sampling (>20), we would want a different UI/comparison approach
	const maxSampleSize = 20
	if req.SampleSize > maxSampleSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("sample_size cannot exceed %d", maxSampleSize))
		return
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
				Limit:    defaultAxQueryLimit,
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
		writeJSON(w, http.StatusOK, resp)
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

	// Aggregate error tracking
	var successCount, failureCount int
	for _, sample := range samples {
		if sample.Error != "" {
			failureCount++
		} else {
			successCount++
		}
	}

	// Build response
	response := PromptPreviewResponse{
		TotalAttestations: totalAttestations,
		SampleSize:        actualSampleSize,
		Samples:           samples,
		SuccessCount:      successCount,
		FailureCount:      failureCount,
	}

	// Set global error if all samples failed
	if failureCount > 0 && successCount == 0 {
		response.Error = fmt.Sprintf("All %d samples failed. Check individual sample errors for details.", failureCount)
	}

	writeJSON(w, http.StatusOK, response)
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
	if err := readJSON(w, r, &req); err != nil {
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

	writeJSON(w, http.StatusOK, resp)
}

// createPromptAIClient creates an AI client for prompt execution
func (s *QNTXServer) createPromptAIClient(providerName, model string) provider.AIClient {
	return s.createAIClient(providerName, model, "prompt-execute")
}

// HandlePromptDirect handles POST /api/prompt/direct
// Executes a prompt template directly without attestation interpolation
func (s *QNTXServer) HandlePromptDirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req PromptDirectRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if req.Template == "" {
		writeError(w, http.StatusBadRequest, "template is required")
		return
	}

	// Parse frontmatter to extract config
	doc, err := prompt.ParseFrontmatter(req.Template)
	if err != nil {
		wrappedErr := errors.Wrap(err, "failed to parse frontmatter")
		writeWrappedError(w, s.logger, wrappedErr, "Failed to parse frontmatter", http.StatusBadRequest)
		return
	}

	// Interpolate template with upstream attestation if provided
	var promptText string
	if req.UpstreamAttestation != nil {
		tmpl, err := prompt.Parse(doc.Body)
		if err != nil {
			// No valid placeholders — use body as-is
			promptText = doc.Body
		} else {
			interpolated, err := tmpl.Execute(req.UpstreamAttestation)
			if err != nil {
				wrappedErr := errors.Wrapf(err, "failed to interpolate template with upstream attestation %s", req.UpstreamAttestation.ID)
				writeWrappedError(w, s.logger, wrappedErr, "Template interpolation failed", http.StatusBadRequest)
				return
			}
			promptText = interpolated
		}
	} else {
		promptText = doc.Body
	}

	// Determine model (request > frontmatter > config default)
	modelName := req.Model
	if modelName == "" && doc.Metadata.Model != "" {
		modelName = doc.Metadata.Model
	}

	// Create AI client
	client := s.createAIClient(req.Provider, modelName, "prompt-direct")

	// Call LLM using Chat method
	chatReq := openrouter.ChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   promptText,
	}

	// Set temperature if specified in frontmatter
	if doc.Metadata.Temperature != nil {
		chatReq.Temperature = doc.Metadata.Temperature
	}

	// Set max tokens if specified in frontmatter
	if doc.Metadata.MaxTokens != nil {
		chatReq.MaxTokens = doc.Metadata.MaxTokens
	}

	// Build multimodal attachments from file IDs (melded Doc glyphs)
	if len(req.FileIDs) > 0 {
		for _, fid := range req.FileIDs {
			mime, b64, readErr := s.readFileBase64(fid)
			if readErr != nil {
				s.logger.Warnw("Skipping attached file",
					"file_id", fid, "error", errors.Wrapf(readErr, "failed to read attachment %s", fid))
				continue
			}

			switch {
			case strings.HasPrefix(mime, "image/"):
				chatReq.Attachments = append(chatReq.Attachments, openrouter.ContentPart{
					Type: "image_url",
					ImageURL: &openrouter.ContentPartImage{
						URL: "data:" + mime + ";base64," + b64,
					},
				})
			case mime == "application/pdf":
				chatReq.Attachments = append(chatReq.Attachments, openrouter.ContentPart{
					Type: "file",
					File: &openrouter.ContentPartFile{
						Filename: fid,
						FileData: "data:" + mime + ";base64," + b64,
					},
				})
			default:
				s.logger.Warnw("Unsupported MIME type for LLM attachment, skipping",
					"file_id", fid, "mime", mime)
				continue
			}

			s.logger.Debugw("Attached file to prompt",
				"file_id", fid, "mime", mime, "size_kb", len(b64)*3/4/1024)
		}
	}

	// Execute prompt
	resp, err := client.Chat(r.Context(), chatReq)
	if err != nil {
		s.logger.Errorw("Prompt direct execution failed",
			"error", err,
			"provider", req.Provider,
		)
		writeWrappedError(w, s.logger, err, "Prompt execution failed", http.StatusInternalServerError)
		return
	}

	// Create prompt-result attestation so the response is discoverable in the graph
	var attestationID string
	if req.GlyphID != "" {
		actor := "glyph:" + req.GlyphID
		subject := modelName
		if subject == "" {
			subject = "unknown-model"
		}

		asid, asidErr := id.GenerateASID(subject, "prompt-result", req.GlyphID, actor)
		if asidErr != nil {
			s.logger.Warnw("Failed to generate ASID for prompt-result attestation",
				"glyph_id", req.GlyphID, "error", asidErr)
		} else {
			now := time.Now()
			attrs := map[string]interface{}{
				"response": resp.Content,
				"template": req.Template,
			}
			as := &types.As{
				ID:         asid,
				Subjects:   []string{subject},
				Predicates: []string{"prompt-result"},
				Contexts:   []string{req.GlyphID},
				Actors:     []string{actor},
				Timestamp:  now,
				Source:     "prompt-direct",
				Attributes: attrs,
				CreatedAt:  now,
			}

			store := storage.NewSQLStore(s.db, s.logger)
			if storeErr := store.CreateAttestation(as); storeErr != nil {
				s.logger.Warnw("Failed to create prompt-result attestation",
					"glyph_id", req.GlyphID, "asid", asid, "error", storeErr)
			} else {
				attestationID = asid
				s.logger.Infow("Created prompt-result attestation",
					"asid", asid, "subject", subject, "glyph_id", req.GlyphID)
			}
		}
	}

	// Return response
	response := PromptDirectResponse{
		Response:         resp.Content,
		AttestationID:    attestationID,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}

	writeJSON(w, http.StatusOK, response)
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
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

	writeJSON(w, http.StatusOK, p)
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
	if err := readJSON(w, r, &req); err != nil {
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

	writeJSON(w, http.StatusCreated, saved)
}

// sampleAttestations randomly samples n attestations from the provided list using Fisher-Yates shuffle.
//
// TODO(non-deterministic-sampling): This function uses unseeded math/rand, violating QNTX's
// deterministic operations standard (CLAUDE.md). However, this is a deliberate tradeoff worth
// discussing:
//
// THE PARADOX: LLMs are inherently non-deterministic. This X-sampling feature exists precisely
// BECAUSE of that non-determinism - we're trying to work WITH it, not against it. The entire
// point of preview sampling is to test prompt behavior across diverse inputs to build confidence
// before production deployment. Higher X = more samples = higher confidence that the prompt
// behaves correctly across the attestation space.
//
// THE TRADEOFF:
//   - Reproducibility: Unseeded random means identical API calls produce different samples
//   - Purpose: Random sampling is the FEATURE - we want diverse coverage, not the same N every time
//   - Debugging: Non-reproducible results make it harder to debug specific failures
//
// POTENTIAL SOLUTIONS (choose based on use case priority):
//
//  1. Add optional 'seed' parameter to API request
//     - Pros: Reproducible when needed, random by default
//     - Cons: Additional API complexity, users must understand seeding
//
//  2. Use deterministic sampling (first N, evenly spaced, hash-based)
//     - Pros: Fully reproducible, simpler
//     - Cons: Loses randomness benefit, may miss edge cases clustered in unsampled regions
//
//  3. Use crypto/rand for cryptographically secure randomness
//     - Pros: More secure random
//     - Cons: Still non-reproducible, overkill for this use case
//
//  4. Accept non-determinism as a feature
//     - Pros: Embraces the purpose of X-sampling
//     - Cons: Violates QNTX standards, harder debugging
//
// RECOMMENDATION: Add optional 'seed' parameter (solution 1) to balance reproducibility needs
// with the feature's purpose. Default to time-seeded random, allow explicit seed for debugging.
//
// TODO(issue #342): Implement deterministic sampling option
//
// SECURITY NOTE: math/rand is sufficient here - we're sampling attestations, not generating
// cryptographic material. Predictability is not a security concern in this context.
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
	// Determine model (request > frontmatter > config default)
	model := req.Model
	if model == "" && doc.Metadata.Model != "" {
		model = doc.Metadata.Model
	}
	return s.createAIClient(req.Provider, model, "prompt-preview")
}

// createAIClient creates an AI client using config defaults with optional overrides.
// providerName selects "local" or "openrouter" (empty = auto-detect from config).
// model overrides the configured model (empty = use config default).
// operationType is used for usage tracking (e.g., "prompt-execute", "prompt-preview").
func (s *QNTXServer) createAIClient(providerName, model, operationType string) provider.AIClient {
	localEnabled := appcfg.GetBool("local_inference.enabled")
	localBaseURL := appcfg.GetString("local_inference.base_url")
	localModel := appcfg.GetString("local_inference.model")
	localTimeout := appcfg.GetInt("local_inference.timeout_seconds")
	openRouterAPIKey := appcfg.GetString("openrouter.api_key")
	openRouterModel := appcfg.GetString("openrouter.model")

	useLocal := providerName == "local" || (providerName == "" && localEnabled)

	if useLocal {
		effectiveModel := model
		if effectiveModel == "" {
			effectiveModel = localModel
		}
		if effectiveModel == "" {
			effectiveModel = defaultLocalModel
		}
		return provider.NewLocalClient(provider.LocalClientConfig{
			BaseURL:        localBaseURL,
			Model:          effectiveModel,
			TimeoutSeconds: localTimeout,
			DB:             s.db,
			OperationType:  operationType,
		})
	}

	effectiveModel := model
	if effectiveModel == "" {
		effectiveModel = openRouterModel
	}
	if effectiveModel == "" {
		effectiveModel = defaultOpenRouterModel
	}
	return openrouter.NewClient(openrouter.Config{
		APIKey:        openRouterAPIKey,
		Model:         effectiveModel,
		DB:            s.db,
		OperationType: operationType,
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
	case path == "/direct":
		s.HandlePromptDirect(w, r)
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
