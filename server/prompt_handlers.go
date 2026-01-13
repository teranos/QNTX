package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/ai/provider"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/logger"
)

// PromptPreviewRequest represents a request to preview ax query results
type PromptPreviewRequest struct {
	AxQuery string `json:"ax_query"`
}

// PromptPreviewResponse represents the preview response
type PromptPreviewResponse struct {
	AttestationCount int                      `json:"attestation_count"`
	Attestations     []map[string]interface{} `json:"attestations,omitempty"`
	Error            string                   `json:"error,omitempty"`
}

// PromptExecuteRequest represents a request to execute a prompt
type PromptExecuteRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Provider     string `json:"provider,omitempty"` // "openrouter" or "local"
	Model        string `json:"model,omitempty"`
}

// PromptExecuteResponse represents the execution response
type PromptExecuteResponse struct {
	Results          []prompt.Result `json:"results"`
	AttestationCount int             `json:"attestation_count"`
	Error            string          `json:"error,omitempty"`
}

// HandlePromptPreview handles POST /api/prompt/preview
// Returns attestations matching the ax query for preview
func (s *QNTXServer) HandlePromptPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	logger.AddAxSymbol(s.logger).Infow("Prompt preview request")

	var req PromptPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if strings.TrimSpace(req.AxQuery) == "" {
		writeError(w, http.StatusBadRequest, "ax_query is required")
		return
	}

	// Parse the ax query
	args := strings.Fields(req.AxQuery)
	filter, err := parser.ParseAxCommandWithContext(args, 0, parser.ErrorContextPlain)
	if err != nil {
		// Check if it's just a warning (best-effort parsing)
		if _, isWarning := err.(*parser.ParseWarning); !isWarning {
			writeError(w, http.StatusBadRequest, "Invalid ax query: "+err.Error())
			return
		}
	}

	// Execute the query using storage executor
	executor := storage.NewExecutor(s.db)
	result, err := executor.ExecuteAsk(r.Context(), *filter)
	if err != nil {
		writeWrappedError(w, s.logger, err, "Failed to execute ax query", http.StatusInternalServerError)
		return
	}

	// Convert attestations to map format for JSON response
	attestations := make([]map[string]interface{}, len(result.Attestations))
	for i, as := range result.Attestations {
		attestations[i] = map[string]interface{}{
			"id":         as.ID,
			"subjects":   as.Subjects,
			"predicates": as.Predicates,
			"contexts":   as.Contexts,
			"actors":     as.Actors,
			"timestamp":  as.Timestamp,
			"source":     as.Source,
			"attributes": as.Attributes,
		}
	}

	resp := PromptPreviewResponse{
		AttestationCount: len(result.Attestations),
		Attestations:     attestations,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
		writeError(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
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
		writeError(w, http.StatusBadRequest, "Invalid template: "+err.Error())
		return
	}

	// Parse the ax query
	args := strings.Fields(req.AxQuery)
	filter, err := parser.ParseAxCommandWithContext(args, 0, parser.ErrorContextPlain)
	if err != nil {
		if _, isWarning := err.(*parser.ParseWarning); !isWarning {
			writeError(w, http.StatusBadRequest, "Invalid ax query: "+err.Error())
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
	results, err := prompt.ExecuteOneShot(
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

// HandlePrompt routes prompt-related requests
func (s *QNTXServer) HandlePrompt(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/prompt")

	switch {
	case path == "/preview":
		s.HandlePromptPreview(w, r)
	case path == "/execute":
		s.HandlePromptExecute(w, r)
	default:
		writeError(w, http.StatusNotFound, "Unknown prompt endpoint")
	}
}

// executePromptAxQuery is a helper that executes an ax query and returns the result
func (s *QNTXServer) executePromptAxQuery(ctx context.Context, filter types.AxFilter) (*ax.AxResult, error) {
	executor := storage.NewExecutor(s.db)
	return executor.ExecuteAsk(ctx, filter)
}
