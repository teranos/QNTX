package qntxopenrouter

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	vanity "github.com/teranos/vanity-id"
)

const (
	defaultAxQueryLimit    = 100
	defaultOpenRouterModel = "openai/gpt-4o-mini"
	maxSampleSize          = 20
)

// Handlers holds the HTTP handlers for the OpenRouter plugin.
type Handlers struct {
	plugin *Plugin
}

// PromptPreviewRequest represents a request to preview prompt execution with X-sampling
type PromptPreviewRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	SampleSize   int    `json:"sample_size,omitempty"`
	Model        string `json:"model,omitempty"`
}

// PreviewSample represents a single sample execution result
type PreviewSample struct {
	Attestation        map[string]interface{} `json:"attestation"`
	InterpolatedPrompt string                 `json:"interpolated_prompt"`
	Response           string                 `json:"response"`
	PromptTokens       int                    `json:"prompt_tokens,omitempty"`
	CompletionTokens   int                    `json:"completion_tokens,omitempty"`
	TotalTokens        int                    `json:"total_tokens,omitempty"`
	Error              string                 `json:"error,omitempty"`
}

// PromptPreviewResponse represents the preview response
type PromptPreviewResponse struct {
	TotalAttestations int             `json:"total_attestations"`
	SampleSize        int             `json:"sample_size"`
	Samples           []PreviewSample `json:"samples"`
	SuccessCount      int             `json:"success_count"`
	FailureCount      int             `json:"failure_count"`
	Error             string          `json:"error,omitempty"`
}

// PromptExecuteRequest represents a request to execute a prompt
type PromptExecuteRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Model        string `json:"model,omitempty"`
}

// PromptExecuteResponse represents the execution response
type PromptExecuteResponse struct {
	Results          []PromptResult `json:"results"`
	AttestationCount int            `json:"attestation_count"`
	Error            string         `json:"error,omitempty"`
}

// PromptResult represents a single prompt execution result
type PromptResult struct {
	SourceAttestationID string `json:"source_attestation_id"`
	Prompt              string `json:"prompt"`
	Response            string `json:"response"`
	ResultAttestationID string `json:"result_attestation_id,omitempty"`
	PromptTokens        int    `json:"prompt_tokens,omitempty"`
	CompletionTokens    int    `json:"completion_tokens,omitempty"`
	TotalTokens         int    `json:"total_tokens,omitempty"`
}

// PromptDirectRequest represents a request to execute a prompt directly
type PromptDirectRequest struct {
	Template            string    `json:"template"`
	SystemPrompt        string    `json:"system_prompt,omitempty"`
	Model               string    `json:"model,omitempty"`
	GlyphID             string    `json:"glyph_id,omitempty"`
	UpstreamAttestation *types.As `json:"upstream_attestation,omitempty"`
	FileIDs             []string  `json:"file_ids,omitempty"`
}

// PromptDirectResponse represents the direct execution response
type PromptDirectResponse struct {
	Response         string                `json:"response"`
	AttestationID    string                `json:"attestation_id,omitempty"`
	Attestation      *protocol.Attestation `json:"attestation,omitempty"`
	PromptTokens     int                   `json:"prompt_tokens,omitempty"`
	CompletionTokens int                   `json:"completion_tokens,omitempty"`
	TotalTokens      int                   `json:"total_tokens,omitempty"`
	Error            string                `json:"error,omitempty"`
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

// HandlePromptRoute routes /prompt/{id} and /prompt/{name}/versions requests
func (h *Handlers) HandlePromptRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/prompt")

	switch {
	case path == "/preview":
		h.HandlePromptPreview(w, r)
	case path == "/execute":
		h.HandlePromptExecute(w, r)
	case path == "/direct":
		h.HandlePromptDirect(w, r)
	case path == "/list":
		h.HandlePromptList(w, r)
	case path == "/save":
		h.HandlePromptSave(w, r)
	case strings.HasSuffix(path, "/versions"):
		name := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/versions")
		h.handlePromptVersions(w, r, name)
	case strings.HasPrefix(path, "/"):
		promptID := strings.TrimPrefix(path, "/")
		if promptID != "" {
			h.handlePromptGet(w, r, promptID)
			return
		}
		fallthrough
	default:
		writeError(w, http.StatusNotFound, "Unknown prompt endpoint")
	}
}

// HandlePromptPreview handles POST /prompt/preview
func (h *Handlers) HandlePromptPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req PromptPreviewRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	if strings.TrimSpace(req.AxQuery) == "" {
		writeError(w, http.StatusBadRequest, "ax_query is required")
		return
	}
	if strings.TrimSpace(req.Template) == "" {
		writeError(w, http.StatusBadRequest, "template is required")
		return
	}

	if req.SampleSize <= 0 {
		req.SampleSize = 1
	}
	if req.SampleSize > maxSampleSize {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("sample_size cannot exceed %d", maxSampleSize))
		return
	}

	// Parse ax query
	var filter *types.AxFilter
	args := strings.Fields(req.AxQuery)
	parsedFilter, err := parser.ParseAxCommandWithContext(args, 0, parser.ErrorContextPlain)
	if err != nil {
		if _, isWarning := err.(*parser.ParseWarning); !isWarning {
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

	// Execute the query
	db := h.plugin.Services().Database()
	executor := storage.NewExecutor(db)
	result, err := executor.ExecuteAsk(r.Context(), *filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to execute ax query: %v", err))
		return
	}

	totalAttestations := len(result.Attestations)
	if totalAttestations == 0 {
		writeJSON(w, http.StatusOK, PromptPreviewResponse{
			TotalAttestations: 0,
			SampleSize:        req.SampleSize,
			Samples:           []PreviewSample{},
		})
		return
	}

	// Parse frontmatter and template
	doc, err := prompt.ParseFrontmatter(req.Template)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse frontmatter: %v", err))
		return
	}
	tmpl, err := prompt.Parse(doc.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse template: %v", err))
		return
	}

	// X-sampling
	actualSampleSize := req.SampleSize
	if actualSampleSize > totalAttestations {
		actualSampleSize = totalAttestations
	}
	sampledAttestations := sampleAttestations(result.Attestations, actualSampleSize)

	// Build client with model override
	client := h.buildClient(req.Model, doc)

	// Process each sampled attestation
	samples := make([]PreviewSample, len(sampledAttestations))
	for i, as := range sampledAttestations {
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

		interpolatedPrompt, err := tmpl.Execute(&as)
		if err != nil {
			samples[i] = PreviewSample{
				Attestation: attestationMap,
				Error:       fmt.Sprintf("Failed to interpolate template: %v", err),
			}
			continue
		}

		chatReq := ChatRequest{
			SystemPrompt: req.SystemPrompt,
			UserPrompt:   interpolatedPrompt,
		}
		if req.Model != "" {
			chatReq.Model = &req.Model
		} else if doc.Metadata.Model != "" {
			chatReq.Model = &doc.Metadata.Model
		}
		if doc.Metadata.Temperature != nil {
			chatReq.Temperature = doc.Metadata.Temperature
		}
		if doc.Metadata.MaxTokens != nil {
			chatReq.MaxTokens = doc.Metadata.MaxTokens
		}

		resp, err := client.Chat(r.Context(), chatReq)
		if err != nil {
			samples[i] = PreviewSample{
				Attestation:        attestationMap,
				InterpolatedPrompt: interpolatedPrompt,
				Error:              fmt.Sprintf("LLM call failed: %v", err),
			}
			continue
		}

		samples[i] = PreviewSample{
			Attestation:        attestationMap,
			InterpolatedPrompt: interpolatedPrompt,
			Response:           resp.Content,
			PromptTokens:       resp.Usage.PromptTokens,
			CompletionTokens:   resp.Usage.CompletionTokens,
			TotalTokens:        resp.Usage.TotalTokens,
		}
	}

	var successCount, failureCount int
	for _, sample := range samples {
		if sample.Error != "" {
			failureCount++
		} else {
			successCount++
		}
	}

	response := PromptPreviewResponse{
		TotalAttestations: totalAttestations,
		SampleSize:        actualSampleSize,
		Samples:           samples,
		SuccessCount:      successCount,
		FailureCount:      failureCount,
	}

	if failureCount > 0 && successCount == 0 {
		response.Error = fmt.Sprintf("All %d samples failed. Check individual sample errors for details.", failureCount)
	}

	writeJSON(w, http.StatusOK, response)
}

// HandlePromptExecute handles POST /prompt/execute
func (h *Handlers) HandlePromptExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req PromptExecuteRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	if strings.TrimSpace(req.AxQuery) == "" {
		writeError(w, http.StatusBadRequest, "ax_query is required")
		return
	}
	if strings.TrimSpace(req.Template) == "" {
		writeError(w, http.StatusBadRequest, "template is required")
		return
	}

	if err := prompt.ValidateTemplate(req.Template); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid template: %v", err))
		return
	}

	// Parse ax query
	args := strings.Fields(req.AxQuery)
	filter, err := parser.ParseAxCommandWithContext(args, 0, parser.ErrorContextPlain)
	if err != nil {
		if _, isWarning := err.(*parser.ParseWarning); !isWarning {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid ax query: %v", err))
			return
		}
	}

	// Parse frontmatter
	doc, err := prompt.ParseFrontmatter(req.Template)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse frontmatter: %v", err))
		return
	}
	tmpl, err := prompt.Parse(doc.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse template: %v", err))
		return
	}

	// Execute query
	db := h.plugin.Services().Database()
	executor := storage.NewExecutor(db)
	result, err := executor.ExecuteAsk(r.Context(), *filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to execute ax query: %v", err))
		return
	}

	if len(result.Attestations) == 0 {
		writeJSON(w, http.StatusOK, PromptExecuteResponse{
			Results:          []PromptResult{},
			AttestationCount: 0,
		})
		return
	}

	client := h.buildClient(req.Model, doc)

	var results []PromptResult
	for _, as := range result.Attestations {
		interpolated, err := tmpl.Execute(&as)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to interpolate template for %s: %v", as.ID, err))
			return
		}

		chatReq := ChatRequest{
			SystemPrompt: req.SystemPrompt,
			UserPrompt:   interpolated,
		}
		if doc.Metadata.Temperature != nil {
			chatReq.Temperature = doc.Metadata.Temperature
		}
		if doc.Metadata.MaxTokens != nil {
			chatReq.MaxTokens = doc.Metadata.MaxTokens
		}
		if doc.Metadata.Model != "" {
			chatReq.Model = &doc.Metadata.Model
		}

		resp, err := client.Chat(r.Context(), chatReq)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("LLM call failed for %s: %v", as.ID, err))
			return
		}

		results = append(results, PromptResult{
			SourceAttestationID: as.ID,
			Prompt:              interpolated,
			Response:            resp.Content,
			PromptTokens:        resp.Usage.PromptTokens,
			CompletionTokens:    resp.Usage.CompletionTokens,
			TotalTokens:         resp.Usage.TotalTokens,
		})
	}

	writeJSON(w, http.StatusOK, PromptExecuteResponse{
		Results:          results,
		AttestationCount: len(results),
	})
}

// HandlePromptDirect handles POST /prompt/direct
func (h *Handlers) HandlePromptDirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req PromptDirectRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
		return
	}

	if req.Template == "" {
		writeError(w, http.StatusBadRequest, "template is required")
		return
	}

	doc, err := prompt.ParseFrontmatter(req.Template)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse frontmatter: %v", err))
		return
	}

	// Interpolate template with upstream attestation if provided
	var promptText string
	if req.UpstreamAttestation != nil {
		tmpl, err := prompt.Parse(doc.Body)
		if err != nil {
			promptText = doc.Body
		} else {
			interpolated, err := tmpl.Execute(req.UpstreamAttestation)
			if err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Template interpolation failed: %v", err))
				return
			}
			promptText = interpolated
		}
	} else {
		promptText = doc.Body
	}

	modelName := req.Model
	if modelName == "" && doc.Metadata.Model != "" {
		modelName = doc.Metadata.Model
	}

	client := h.buildClient(modelName, doc)

	chatReq := ChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   promptText,
	}
	if doc.Metadata.Temperature != nil {
		chatReq.Temperature = doc.Metadata.Temperature
	}
	if doc.Metadata.MaxTokens != nil {
		chatReq.MaxTokens = doc.Metadata.MaxTokens
	}

	// Build multimodal attachments from file IDs
	if len(req.FileIDs) > 0 {
		logger := h.plugin.Services().Logger("openrouter")
		db := h.plugin.Services().Database()
		for _, fid := range req.FileIDs {
			mime, b64, readErr := readFileBase64(db, fid)
			if readErr != nil {
				logger.Warnw("Skipping attached file",
					"file_id", fid, "error", readErr)
				continue
			}

			switch {
			case strings.HasPrefix(mime, "image/"):
				chatReq.Attachments = append(chatReq.Attachments, ContentPart{
					Type: "image_url",
					ImageURL: &ContentPartImage{
						URL: "data:" + mime + ";base64," + b64,
					},
				})
			case mime == "application/pdf":
				chatReq.Attachments = append(chatReq.Attachments, ContentPart{
					Type: "file",
					File: &ContentPartFile{
						Filename: fid,
						FileData: "data:" + mime + ";base64," + b64,
					},
				})
			default:
				logger.Warnw("Unsupported MIME type for LLM attachment, skipping",
					"file_id", fid, "mime", mime)
			}
		}
	}

	resp, err := client.Chat(r.Context(), chatReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Prompt execution failed: %v", err))
		return
	}

	// Create prompt-result attestation if glyph context exists
	var attestationID string
	var createdAttestation *protocol.Attestation
	if req.GlyphID != "" {
		attestationID, createdAttestation = h.createResultAttestation(
			req.GlyphID, modelName, req.Template, resp.Content)
	}

	writeJSON(w, http.StatusOK, PromptDirectResponse{
		Response:         resp.Content,
		AttestationID:    attestationID,
		Attestation:      createdAttestation,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	})
}

// HandlePromptList handles GET /prompt/list
func (h *Handlers) HandlePromptList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	db := h.plugin.Services().Database()
	store := prompt.NewPromptStore(db)
	prompts, err := store.ListPrompts(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to list prompts: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"prompts": prompts,
		"count":   len(prompts),
	})
}

// HandlePromptSave handles POST /prompt/save
func (h *Handlers) HandlePromptSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req PromptSaveRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
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

	db := h.plugin.Services().Database()
	store := prompt.NewPromptStore(db)
	storedPrompt := &prompt.StoredPrompt{
		Name:         req.Name,
		Template:     req.Template,
		SystemPrompt: req.SystemPrompt,
		AxPattern:    req.AxPattern,
		Provider:     req.Provider,
		Model:        req.Model,
	}

	saved, err := store.SavePrompt(r.Context(), storedPrompt, "user")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save prompt: %v", err))
		return
	}

	writeJSON(w, http.StatusCreated, saved)
}

// handlePromptGet handles GET /prompt/{id}
func (h *Handlers) handlePromptGet(w http.ResponseWriter, r *http.Request, promptID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	db := h.plugin.Services().Database()
	store := prompt.NewPromptStore(db)
	p, err := store.GetPromptByID(r.Context(), promptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get prompt: %v", err))
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "Prompt not found")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// handlePromptVersions handles GET /prompt/{name}/versions
func (h *Handlers) handlePromptVersions(w http.ResponseWriter, r *http.Request, promptName string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	db := h.plugin.Services().Database()
	store := prompt.NewPromptStore(db)
	versions, err := store.GetPromptVersions(r.Context(), promptName, 16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get prompt versions: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"versions": versions,
		"count":    len(versions),
	})
}

// buildClient creates an OpenRouter client with optional model override from request/frontmatter
func (h *Handlers) buildClient(modelOverride string, doc *prompt.PromptDocument) *Client {
	config := h.plugin.Services().Config("openrouter")
	apiKey := config.GetString("api_key")

	model := modelOverride
	if model == "" && doc != nil && doc.Metadata.Model != "" {
		model = doc.Metadata.Model
	}
	if model == "" {
		model = config.GetString("model")
	}
	if model == "" {
		model = DefaultModel
	}

	return NewClient(Config{
		APIKey:        apiKey,
		Model:         model,
		Logger:        h.plugin.Services().Logger("openrouter"),
		DB:            h.plugin.Services().Database(),
		OperationType: "prompt",
	})
}

// createResultAttestation creates a prompt-result attestation in the ATS
func (h *Handlers) createResultAttestation(glyphID, modelName, template, response string) (string, *protocol.Attestation) {
	logger := h.plugin.Services().Logger("openrouter")

	actor := "glyph:" + glyphID
	subject := modelName
	if subject == "" {
		subject = "unknown-model"
	}

	asid, err := vanity.GenerateASID(subject, "prompt-result", glyphID, actor)
	if err != nil {
		logger.Warnw("Failed to generate ASID for prompt-result attestation",
			"glyph_id", glyphID, "error", err)
		return "", nil
	}

	now := time.Now()
	attrs := map[string]interface{}{
		"response": response,
		"template": template,
	}
	as := &types.As{
		ID:         asid,
		Subjects:   []string{subject},
		Predicates: []string{"prompt-result"},
		Contexts:   []string{glyphID},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     "prompt-direct",
		Attributes: attrs,
		CreatedAt:  now,
	}

	store := h.plugin.Services().ATSStore()
	if storeErr := store.CreateAttestation(as); storeErr != nil {
		logger.Warnw("Failed to create prompt-result attestation",
			"glyph_id", glyphID, "asid", asid, "error", storeErr)
		return "", nil
	}

	logger.Infow("Created prompt-result attestation",
		"asid", asid, "subject", subject, "glyph_id", glyphID)

	protoAs, convErr := protocol.AttestationFromTypes(as)
	if convErr != nil {
		logger.Warnw("Failed to convert attestation to protocol format",
			"asid", asid, "error", convErr)
		return asid, nil
	}

	return asid, protoAs
}

// sampleAttestations randomly samples n attestations using Fisher-Yates shuffle
func sampleAttestations(attestations []types.As, n int) []types.As {
	if n >= len(attestations) {
		return attestations
	}

	sampled := make([]types.As, len(attestations))
	copy(sampled, attestations)

	for i := 0; i < n; i++ {
		j := i + rand.Intn(len(sampled)-i)
		sampled[i], sampled[j] = sampled[j], sampled[i]
	}

	return sampled[:n]
}

// readFileBase64 reads a stored file and returns its MIME type and base64-encoded content.
// File attachments are stored on the filesystem by the core server. The plugin doesn't
// have direct filesystem access yet — this will be implemented via a gRPC file service.
func readFileBase64(_ interface{}, fileID string) (string, string, error) {
	return "", "", errors.Newf("file attachment loading not yet implemented in plugin for %s", fileID)
}

// JSON helpers

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
