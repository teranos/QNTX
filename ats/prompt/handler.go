package prompt

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/ai/provider"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	id "github.com/teranos/vanity-id"
)

// HandlerName is the registered name for the prompt handler
const HandlerName = "prompt.execute"

// Payload represents the job payload for prompt execution
type Payload struct {
	// AxFilter defines which attestations to query
	AxFilter types.AxFilter `json:"ax_filter"`

	// Template is the prompt template with {{field}} placeholders
	Template string `json:"template"`

	// SystemPrompt is the optional system instruction for the LLM
	SystemPrompt string `json:"system_prompt,omitempty"`

	// TemporalCursor tracks the last processed timestamp for incremental processing
	// On first run, this is zero. On subsequent runs, only attestations newer than
	// this cursor are processed.
	TemporalCursor *time.Time `json:"temporal_cursor,omitempty"`

	// Provider specifies which LLM provider to use: "openrouter" or "local"
	Provider string `json:"provider,omitempty"`

	// Model overrides the default model for the provider
	Model string `json:"model,omitempty"`

	// PromptID is the attestation ID of the stored prompt being executed
	// Used to construct actor as model@promptID
	PromptID string `json:"prompt_id,omitempty"`

	// ResultPredicate is the predicate used when creating result attestations
	// Defaults to "prompt-result"
	ResultPredicate string `json:"result_predicate,omitempty"`

	// ResultActor is the actor for result attestations
	// Defaults to model@promptID (or just model if no PromptID)
	ResultActor string `json:"result_actor,omitempty"`
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

	// Usage tracks token usage
	Usage openrouter.Usage `json:"usage,omitempty"`
}

// Handler implements async.JobHandler for prompt execution
type Handler struct {
	queryStore    ats.AttestationQueryStore
	store         ats.AttestationStore
	aliasResolver *alias.Resolver
	config        *am.Config
	db            *sql.DB
}

// NewHandler creates a new prompt handler
func NewHandler(
	queryStore ats.AttestationQueryStore,
	store ats.AttestationStore,
	aliasResolver *alias.Resolver,
	config *am.Config,
	db *sql.DB,
) *Handler {
	return &Handler{
		queryStore:    queryStore,
		store:         store,
		aliasResolver: aliasResolver,
		config:        config,
		db:            db,
	}
}

// Name returns the handler name for registration
func (h *Handler) Name() string {
	return HandlerName
}

// Execute runs the prompt job
func (h *Handler) Execute(ctx context.Context, job *async.Job) error {
	// Decode payload
	var payload Payload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return errors.Wrap(err, "failed to decode prompt payload")
	}

	// Parse frontmatter from template
	doc, err := ParseFrontmatter(payload.Template)
	if err != nil {
		logger.AddAxSymbol(logger.Logger).Errorw("Frontmatter parsing failed",
			"error", err,
			"template_length", len(payload.Template),
		)
		return errors.Wrap(err, "failed to parse frontmatter")
	}

	// Parse template body (after frontmatter)
	tmpl, err := Parse(doc.Body)
	if err != nil {
		logger.AddAxSymbol(logger.Logger).Errorw("Template parsing failed",
			"error", err,
			"template_length", len(doc.Body),
		)
		return errors.Wrap(err, "failed to parse prompt template")
	}

	// Apply temporal cursor filter for incremental processing
	filter := payload.AxFilter
	if payload.TemporalCursor != nil && !payload.TemporalCursor.IsZero() {
		filter.TimeStart = payload.TemporalCursor
	}

	// Execute ax query
	executor := ax.NewAxExecutor(h.queryStore, h.aliasResolver)
	result, err := executor.ExecuteAsk(ctx, filter)
	if err != nil {
		return errors.Wrap(err, "failed to execute ax query")
	}

	if len(result.Attestations) == 0 {
		// No attestations to process - this is not an error
		job.UpdateProgress(0)
		return nil
	}

	// Set total for progress tracking
	job.Progress.Total = len(result.Attestations)

	// Create AI client (using frontmatter metadata or payload overrides)
	client := h.createAIClient(payload, doc)

	// Process each attestation
	var results []Result
	var latestTimestamp time.Time

	for i, as := range result.Attestations {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Interpolate template
		prompt, err := tmpl.Execute(&as)
		if err != nil {
			return errors.Wrapf(err, "failed to interpolate template for attestation %s", as.ID)
		}

		// Call LLM with timing
		// Priority: payload > frontmatter > defaults
		chatReq := openrouter.ChatRequest{
			SystemPrompt: payload.SystemPrompt,
			UserPrompt:   prompt,
		}

		// Model (already handled by createAIClient, but set on request for consistency)
		if payload.Model != "" {
			chatReq.Model = &payload.Model
		} else if doc.Metadata.Model != "" {
			chatReq.Model = &doc.Metadata.Model
		}

		// Temperature from frontmatter
		if doc.Metadata.Temperature != nil {
			chatReq.Temperature = doc.Metadata.Temperature
		}

		// MaxTokens from frontmatter
		if doc.Metadata.MaxTokens != nil {
			chatReq.MaxTokens = doc.Metadata.MaxTokens
		}

		startTime := time.Now()
		resp, err := client.Chat(ctx, chatReq)
		duration := time.Since(startTime)

		if err != nil {
			logger.AddAxSymbol(logger.Logger).Errorw("LLM call failed",
				"error", err,
				"attestation_id", as.ID,
				"duration_ms", duration.Milliseconds(),
			)
			return errors.Wrapf(err, "LLM call failed for attestation %s", as.ID)
		}

		// Log successful LLM call with duration and token usage
		logger.AddAxSymbol(logger.Logger).Infow("LLM call completed",
			"attestation_id", as.ID,
			"duration_ms", duration.Milliseconds(),
			"prompt_tokens", resp.Usage.PromptTokens,
			"completion_tokens", resp.Usage.CompletionTokens,
			"total_tokens", resp.Usage.TotalTokens,
		)

		// Create result attestation
		resultAs, err := h.createResultAttestation(&as, resp.Content, payload)
		if err != nil {
			return errors.Wrapf(err, "failed to create result attestation for %s", as.ID)
		}

		results = append(results, Result{
			SourceAttestationID: as.ID,
			Prompt:              prompt,
			Response:            resp.Content,
			ResultAttestationID: resultAs.ID,
			Usage:               resp.Usage,
		})

		// Track latest timestamp for cursor update
		if as.Timestamp.After(latestTimestamp) {
			latestTimestamp = as.Timestamp
		}

		// Update progress
		job.UpdateProgress(i + 1)
	}

	// Store results in job (as JSON in Source field for now - could add a Results field)
	// The caller can retrieve execution history to see results
	if len(results) > 0 {
		resultsJSON, _ := json.Marshal(results)
		job.Source = string(resultsJSON)
	}

	return nil
}

// createAIClient creates the appropriate AI client based on payload and frontmatter configuration
// Priority: payload.Model > frontmatter.Model > config default
func (h *Handler) createAIClient(payload Payload, doc *PromptDocument) provider.AIClient {
	// Determine model to use (payload overrides frontmatter overrides config)
	model := ""
	if payload.Model != "" {
		model = payload.Model
	} else if doc.Metadata.Model != "" {
		model = doc.Metadata.Model
	}

	// Use provider from payload, or default based on config
	if payload.Provider == "local" || (payload.Provider == "" && h.config.LocalInference.Enabled) {
		clientModel := model
		if clientModel == "" {
			clientModel = h.config.LocalInference.Model
		}
		return provider.NewLocalClient(provider.LocalClientConfig{
			BaseURL:        h.config.LocalInference.BaseURL,
			Model:          clientModel,
			TimeoutSeconds: h.config.LocalInference.TimeoutSeconds,
			DB:             h.db,
			OperationType:  "prompt-execute",
		})
	}

	// Default to OpenRouter
	clientModel := model
	if clientModel == "" {
		clientModel = h.config.OpenRouter.Model
	}
	return openrouter.NewClient(openrouter.Config{
		APIKey:        h.config.OpenRouter.APIKey,
		Model:         clientModel,
		DB:            h.db,
		OperationType: "prompt-execute",
	})
}

// createResultAttestation creates an attestation from the LLM response
func (h *Handler) createResultAttestation(
	sourceAs *types.As,
	response string,
	payload Payload,
) (*types.As, error) {
	predicate := payload.ResultPredicate
	if predicate == "" {
		predicate = "prompt-result"
	}

	actor := payload.ResultActor
	if actor == "" {
		// Determine the model being used
		var model string
		if payload.Model != "" {
			model = payload.Model
		} else if h.config.LocalInference.Enabled {
			model = h.config.LocalInference.Model
		} else {
			model = h.config.OpenRouter.Model
		}

		// Construct actor as model@promptID if we have a prompt ID
		// This represents the actual "agent" - the model running with this specific prompt
		if payload.PromptID != "" {
			actor = model + "@" + payload.PromptID
		} else {
			actor = model
		}
	}

	// Generate ASID: subject, predicate, context, actor
	asid, err := id.GenerateASID(sourceAs.Subjects[0], predicate, sourceAs.ID, actor)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate ASID")
	}

	now := time.Now()
	resultAs := &types.As{
		ID:         asid,
		Subjects:   sourceAs.Subjects, // Same subject as source
		Predicates: []string{predicate},
		Contexts:   []string{sourceAs.ID}, // Context links to source attestation
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     "prompt",
		Attributes: map[string]interface{}{
			"response":       response,
			"source_id":      sourceAs.ID,
			"template":       payload.Template,
			"prompt_handler": HandlerName,
		},
	}

	// Store the attestation
	if err := h.store.CreateAttestation(resultAs); err != nil {
		return nil, errors.Wrap(err, "failed to store result attestation")
	}

	return resultAs, nil
}

// ExecuteOneShot executes a single prompt against specific attestations without scheduling
// This is used by the prompt editor window for testing/iteration
func ExecuteOneShot(
	ctx context.Context,
	queryStore ats.AttestationQueryStore,
	aliasResolver *alias.Resolver,
	client provider.AIClient,
	filter types.AxFilter,
	template string,
	systemPrompt string,
) ([]Result, error) {
	// Parse template
	tmpl, err := Parse(template)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse prompt template")
	}

	// Execute ax query
	executor := ax.NewAxExecutor(queryStore, aliasResolver)
	axResult, err := executor.ExecuteAsk(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "failed to execute ax query")
	}

	if len(axResult.Attestations) == 0 {
		return []Result{}, nil
	}

	// Process each attestation
	var results []Result
	for _, as := range axResult.Attestations {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		// Interpolate template
		prompt, err := tmpl.Execute(&as)
		if err != nil {
			return results, errors.Wrapf(err, "failed to interpolate template for attestation %s", as.ID)
		}

		// Call LLM
		chatReq := openrouter.ChatRequest{
			SystemPrompt: systemPrompt,
			UserPrompt:   prompt,
		}

		resp, err := client.Chat(ctx, chatReq)
		if err != nil {
			return results, errors.Wrapf(err, "LLM call failed for attestation %s", as.ID)
		}

		results = append(results, Result{
			SourceAttestationID: as.ID,
			Prompt:              prompt,
			Response:            resp.Content,
			Usage:               resp.Usage,
		})
	}

	return results, nil
}
