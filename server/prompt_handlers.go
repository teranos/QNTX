package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/teranos/QNTX/ai/provider"
	"github.com/teranos/QNTX/ai/tracker"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

const (
	// Default query limit when parsing ax queries without explicit limit
	defaultAxQueryLimit = 100
)

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
	ParentGlyphID       string    `json:"parent_glyph_id,omitempty"`      // Parent glyph ID for conversation history assembly (stream glyphs send their parent)
	UpstreamAttestation *types.As `json:"upstream_attestation,omitempty"` // Triggering attestation — enables {{field}} interpolation
	FileIDs             []string  `json:"file_ids,omitempty"`             // Attached document/image file IDs for multimodal prompts
}

// PromptDirectResponse represents the direct execution response
type PromptDirectResponse struct {
	Response         string                `json:"response"`
	Model            string                `json:"model,omitempty"`
	AttestationID    string                `json:"attestation_id,omitempty"`
	Attestation      *protocol.Attestation `json:"attestation,omitempty"` // Full attestation with signature
	PromptTokens     int                   `json:"prompt_tokens,omitempty"`
	CompletionTokens int                   `json:"completion_tokens,omitempty"`
	TotalTokens      int                   `json:"total_tokens,omitempty"`
	Error            string                `json:"error,omitempty"`
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

// resolveProvider returns the effective AI provider name.
// Explicit request value takes priority, then llm.provider config, then openrouter default.
func resolveProvider(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if configured := appcfg.GetString("llm.provider"); configured != "" {
		return configured
	}
	return "openrouter"
}

// forwardToProviderPlugin re-encodes the request and forwards it to the named plugin's
// prompt handler. Buffers the response to extract token usage for core-side tracking.
// Returns true if forwarded, false if the provider is local or unknown.
func (s *QNTXServer) forwardToProviderPlugin(w http.ResponseWriter, r *http.Request, providerName string, body any, endpoint string) bool {
	if router := s.servicesManager.GetLLMRouter(); router != nil && router.HasProvider(providerName) {
		return false
	}
	if s.pluginRegistry == nil || !s.pluginRegistry.IsReady(providerName) {
		return false
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		writeWrappedError(w, s.logger, errors.Wrap(err, "failed to re-encode request for plugin"), "Plugin forward failed", http.StatusInternalServerError)
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(encoded))
	r.ContentLength = int64(len(encoded))
	r.URL.Path = "/api/" + providerName + endpoint

	requestTime := time.Now()

	// Buffer the plugin response so we can extract token usage for tracking
	rec := httptest.NewRecorder()
	s.handlePluginRequest(rec, r)

	// Copy buffered response to the real ResponseWriter
	result := rec.Result()
	for k, vals := range result.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(result.StatusCode)
	respBody := rec.Body.Bytes()
	w.Write(respBody)

	// Track usage asynchronously from the buffered response
	if result.StatusCode == http.StatusOK && s.usageTracker != nil {
		go s.trackPluginUsage(respBody, providerName, endpoint, requestTime)
	}

	return true
}

// pluginResponseTokens is a generic shape to extract token counts from any plugin response.
// Works for /prompt/direct (top-level fields) and /prompt/execute (results array).
type pluginResponseTokens struct {
	// Top-level fields (/prompt/direct)
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`

	// Nested array (/prompt/execute)
	Results []struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"results"`
}

// trackPluginUsage parses token counts from a buffered plugin response and records usage.
func (s *QNTXServer) trackPluginUsage(body []byte, providerName, endpoint string, requestTime time.Time) {
	var parsed pluginResponseTokens
	if err := json.Unmarshal(body, &parsed); err != nil {
		s.logger.Debugw("Could not parse plugin response for usage tracking",
			"provider", providerName, "endpoint", endpoint, "error", err)
		return
	}

	promptTokens := parsed.PromptTokens
	completionTokens := parsed.CompletionTokens
	totalTokens := parsed.TotalTokens
	model := parsed.Model

	// Aggregate from results (execute) if top-level is zero
	if totalTokens == 0 {
		for _, r := range parsed.Results {
			promptTokens += r.PromptTokens
			completionTokens += r.CompletionTokens
			totalTokens += r.TotalTokens
		}
	}

	if totalTokens == 0 {
		return // Nothing to track
	}

	responseTime := time.Now()
	cost := tracker.CalculateCost(model, promptTokens, completionTokens)

	// Determine operation type from endpoint (endpoint is e.g. "/prompt/direct")
	opType := "prompt"
	if len(endpoint) > 1 {
		opType = endpoint[1:] // strip leading slash: "prompt/direct", "prompt/preview"
	}

	usage := &tracker.ModelUsage{
		OperationType:     opType,
		EntityType:        "plugin",
		EntityID:          providerName,
		ModelName:         model,
		ModelProvider:     providerName,
		RequestTimestamp:  requestTime,
		ResponseTimestamp: &responseTime,
		TokensUsed:        &totalTokens,
		Cost:              &cost,
		Success:           true,
	}

	if err := s.usageTracker.TrackUsage(usage); err != nil {
		s.logger.Warnw("Failed to track plugin usage",
			"provider", providerName, "endpoint", endpoint, "error", err)
	}
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

	if s.forwardToProviderPlugin(w, r, resolveProvider(req.Provider), req, "/prompt/execute") {
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
	client := s.createPromptAIClient(resolveProvider(req.Provider), req.Model)

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

	// If provider matches a running plugin, forward the entire request to it.
	// The plugin handles frontmatter, attachments, attestations, and the LLM call.
	if s.forwardToProviderPlugin(w, r, resolveProvider(req.Provider), req, "/prompt/direct") {
		return
	}

	// Parse frontmatter to extract config
	doc, err := prompt.ParseFrontmatter(req.Template)
	if err != nil {
		wrappedErr := errors.Wrap(err, "failed to parse frontmatter")
		writeWrappedError(w, s.logger, wrappedErr, "Failed to parse frontmatter", http.StatusBadRequest)
		return
	}

	// Resolve prompt text from template body + optional upstream attestation
	promptText, err := resolvePromptText(doc, req.UpstreamAttestation)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Template interpolation failed", http.StatusBadRequest)
		return
	}

	// Determine model (request > frontmatter > config default)
	modelName := req.Model
	if modelName == "" && doc.Metadata.Model != "" {
		modelName = doc.Metadata.Model
	}

	client := s.createAIClient(resolveProvider(req.Provider), modelName, "prompt-direct")

	chatReq := s.buildDirectChatRequest(r.Context(), req, promptText, modelName, doc)

	// Execute prompt — use streaming if available, fall back to unary
	var resp *provider.ChatResponse

	if streamClient, ok := client.(provider.StreamingAIClient); ok && req.GlyphID != "" {
		resp = s.executeStreamingPrompt(r.Context(), streamClient, chatReq, req.GlyphID, req.Provider)
	} else {
		resp, err = client.Chat(r.Context(), chatReq)
		if err != nil {
			s.logger.Errorw("Prompt direct execution failed",
				"error", err,
				"provider", req.Provider,
			)
			writeWrappedError(w, s.logger, err, "Prompt execution failed", http.StatusInternalServerError)
			return
		}
	}

	// TODO(ATS): After stream completes, create attestation with signal summary
	// (mean confidence, mean entropy, token count, low-confidence spans).
	// See inference-internals.md checklist item ATS.

	// TODO: attestation subject should reflect the actual model that ran, not the
	// requested model from frontmatter. When frontmatter says "anthropic/claude-haiku-4.5"
	// but the request routes through llama.cpp, the attestation subject is wrong.
	// Fix: prefer resp.Model (actual) over req/frontmatter model (requested).
	if modelName == "" && resp.Model != "" {
		modelName = resp.Model
	}

	attestationID, createdAttestation := s.storePromptResultAttestation(resp, req, modelName)

	response := PromptDirectResponse{
		Response:         resp.Content,
		AttestationID:    attestationID,
		Attestation:      createdAttestation,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}

	writeJSON(w, http.StatusOK, response)
}

// resolvePromptText interpolates the template body with an upstream attestation if provided,
// otherwise returns the body as-is.
func resolvePromptText(doc *prompt.PromptDocument, upstream *types.As) (string, error) {
	if upstream == nil {
		return doc.Body, nil
	}
	tmpl, err := prompt.Parse(doc.Body)
	if err != nil {
		// No valid placeholders — use body as-is
		return doc.Body, nil
	}
	interpolated, err := tmpl.Execute(upstream)
	if err != nil {
		return "", errors.Wrapf(err, "failed to interpolate template with upstream attestation %s", upstream.ID)
	}
	return interpolated, nil
}

// buildDirectChatRequest assembles a ChatRequest from the parsed template, conversation
// history, frontmatter metadata, and file attachments.
func (s *QNTXServer) buildDirectChatRequest(ctx context.Context, req PromptDirectRequest, promptText, modelName string, doc *prompt.PromptDocument) provider.ChatRequest {
	conversationMessages := s.assembleConversationHistory(ctx, req)

	chatReq := provider.ChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   promptText,
	}

	// When we have conversation history, use Messages for multi-turn instead of single-turn fields
	if len(conversationMessages) > 0 {
		if req.SystemPrompt != "" {
			chatReq.Messages = append(chatReq.Messages, provider.NewTextMessage("system", req.SystemPrompt))
		}
		chatReq.Messages = append(chatReq.Messages, conversationMessages...)
		chatReq.Messages = append(chatReq.Messages, provider.NewTextMessage("user", promptText))
	}

	if modelName != "" {
		chatReq.Model = &modelName
	}
	if doc.Metadata.Temperature != nil {
		chatReq.Temperature = doc.Metadata.Temperature
	}
	if doc.Metadata.MaxTokens != nil {
		chatReq.MaxTokens = doc.Metadata.MaxTokens
	}

	s.attachFiles(&chatReq, req.FileIDs)

	return chatReq
}

// assembleConversationHistory builds multi-turn message history from the canvas meld graph.
// This is what makes follow-up prompts remember previous turns — the canvas IS the conversation.
func (s *QNTXServer) assembleConversationHistory(ctx context.Context, req PromptDirectRequest) []provider.Message {
	if req.GlyphID == "" || s.conversationAssembler == nil {
		return nil
	}

	// Use parent glyph ID for history assembly when available.
	// Stream glyphs send their own ID as glyph_id (for WebSocket subscription matching)
	// but the parent is the one already persisted in a composition.
	historyGlyphID := req.GlyphID
	if req.ParentGlyphID != "" {
		historyGlyphID = req.ParentGlyphID
	}

	s.logger.Infow("Assembling conversation history",
		"glyph_id", req.GlyphID, "history_glyph_id", historyGlyphID)

	history, err := s.conversationAssembler.AssembleMessages(ctx, historyGlyphID)
	if err != nil {
		s.logger.Warnw("Failed to assemble conversation history, proceeding without",
			"glyph_id", historyGlyphID, "error", err)
		return nil
	}

	if len(history) > 0 {
		s.logger.Infow("Assembled conversation history",
			"glyph_id", historyGlyphID, "message_count", len(history))
	} else {
		s.logger.Infow("No conversation history found", "glyph_id", historyGlyphID)
	}

	return history
}

// attachFiles reads file IDs and appends multimodal content parts (images, PDFs) to the chat request.
func (s *QNTXServer) attachFiles(chatReq *provider.ChatRequest, fileIDs []string) {
	for _, fid := range fileIDs {
		mime, b64, err := s.readFileBase64(fid)
		if err != nil {
			s.logger.Warnw("Skipping attached file",
				"file_id", fid, "error", errors.Wrapf(err, "failed to read attachment %s", fid))
			continue
		}

		switch {
		case strings.HasPrefix(mime, "image/"):
			chatReq.Attachments = append(chatReq.Attachments, provider.ContentPart{
				Type: "image_url",
				ImageURL: &provider.ContentPartImage{
					URL: "data:" + mime + ";base64," + b64,
				},
			})
		case mime == "application/pdf":
			chatReq.Attachments = append(chatReq.Attachments, provider.ContentPart{
				Type: "file",
				File: &provider.ContentPartFile{
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

// storePromptResultAttestation creates a prompt-result attestation so the response
// is discoverable in the graph. Only creates when a glyph context exists.
func (s *QNTXServer) storePromptResultAttestation(resp *provider.ChatResponse, req PromptDirectRequest, modelName string) (string, *protocol.Attestation) {
	if req.GlyphID == "" {
		return "", nil
	}

	actor := "glyph:" + req.GlyphID
	subject := modelName
	if subject == "" {
		subject = "unknown-model"
	}

	asid, err := identity.GenerateASUID("AS", subject, "prompt-result", req.GlyphID)
	if err != nil {
		s.logger.Warnw("Failed to generate ASID for prompt-result attestation",
			"glyph_id", req.GlyphID, "error", err)
		return "", nil
	}

	now := time.Now()
	as := &types.As{
		ID:         asid,
		Subjects:   []string{subject},
		Predicates: []string{"prompt-result"},
		Contexts:   []string{req.GlyphID},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     "prompt-direct",
		Attributes: map[string]interface{}{
			"response": resp.Content,
			"template": req.Template,
		},
		CreatedAt: now,
	}

	if err := s.atsStore.CreateAttestation(as); err != nil {
		s.logger.Warnw("Failed to create prompt-result attestation",
			"glyph_id", req.GlyphID, "asid", asid, "error", err)
		return "", nil
	}

	s.logger.Infow("Created prompt-result attestation",
		"asid", asid, "subject", subject, "glyph_id", req.GlyphID)

	createdAttestation, err := protocol.AttestationFromTypes(as)
	if err != nil {
		s.logger.Warnw("Failed to convert attestation to protocol format",
			"asid", asid, "error", err)
		return asid, nil
	}

	return asid, createdAttestation
}

// executeStreamingPrompt runs the LLM call in streaming mode, broadcasting chunks
// over WebSocket as they arrive. Returns the assembled full response.
func (s *QNTXServer) executeStreamingPrompt(ctx context.Context, streamClient provider.StreamingAIClient, chatReq provider.ChatRequest, glyphID, providerName string) *provider.ChatResponse {
	streamChan := make(chan provider.StreamChunk, 32)

	go func() {
		if streamErr := streamClient.ChatStreaming(ctx, chatReq, streamChan); streamErr != nil {
			s.logger.Errorw("Streaming prompt failed, channel will close",
				"error", streamErr, "provider", providerName)
		}
	}()

	var content strings.Builder
	var streamModel string
	var promptTokens, completionTokens, totalTokens int

	for chunk := range streamChan {
		if chunk.Error != nil {
			s.logger.Errorw("Stream chunk error", "error", chunk.Error)
			continue
		}

		content.WriteString(chunk.Content)

		msg := LLMStreamMessage{
			Type:    "llm_stream",
			JobID:   glyphID,
			Content: chunk.Content,
			Done:    chunk.Done,
			Model:   chunk.Model,
		}

		if chunk.Done {
			msg.PromptTokens = chunk.PromptTokens
			msg.CompletionTokens = chunk.CompletionTokens
			msg.TotalTokens = chunk.TotalTokens
		}

		if chunk.Signal != nil {
			msg.Signal = &LLMTokenSignal{
				Confidence: chunk.Signal.Confidence,
				Entropy:    chunk.Signal.Entropy,
				TopGap:     chunk.Signal.TopGap,
			}
			for _, tc := range chunk.Signal.TopK {
				msg.Signal.TopK = append(msg.Signal.TopK, LLMTokenCandidate{
					ID:   tc.ID,
					Text: tc.Text,
					Prob: tc.Prob,
				})
			}
			for _, stage := range chunk.Signal.SamplerStages {
				ss := SamplerStageSignal{
					Name:        stage.Name,
					ActiveCount: stage.ActiveCount,
					Top1Prob:    stage.Top1Prob,
					Entropy:     stage.Entropy,
				}
				for _, tc := range stage.TopK {
					ss.TopK = append(ss.TopK, LLMTokenCandidate{
						ID:   tc.ID,
						Text: tc.Text,
						Prob: tc.Prob,
					})
				}
				msg.Signal.SamplerStages = append(msg.Signal.SamplerStages, ss)
			}
		}

		s.broadcastLLMStream(msg)

		if chunk.Done {
			streamModel = chunk.Model
			promptTokens = chunk.PromptTokens
			completionTokens = chunk.CompletionTokens
			totalTokens = chunk.TotalTokens
		}
	}

	return &provider.ChatResponse{
		Content: content.String(),
		Model:   streamModel,
		Usage: provider.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		},
	}
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

	store := prompt.NewPromptStore(s.db, s.atsStore)
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

	store := prompt.NewPromptStore(s.db, s.atsStore)
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

	store := prompt.NewPromptStore(s.db, s.atsStore)
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

	store := prompt.NewPromptStore(s.db, s.atsStore)
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

// createAIClient creates a gRPC-backed AI client for the named provider.
func (s *QNTXServer) createAIClient(providerName, model, operationType string) provider.AIClient {
	router := s.servicesManager.GetLLMRouter()
	return provider.NewGRPCLLMClient(router, providerName)
}

// HandlePrompt routes prompt-related requests
func (s *QNTXServer) HandlePrompt(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/prompt")

	switch {
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
