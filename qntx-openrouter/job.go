package qntxopenrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	vanity "github.com/teranos/vanity-id"
)

// PromptExecuteHandlerName is the registered name for the prompt handler
const PromptExecuteHandlerName = "prompt.execute"

// PromptJobPayload represents the job payload for prompt execution
type PromptJobPayload struct {
	AxFilter        types.AxFilter `json:"ax_filter"`
	Template        string         `json:"template"`
	SystemPrompt    string         `json:"system_prompt,omitempty"`
	TemporalCursor  *time.Time     `json:"temporal_cursor,omitempty"`
	Model           string         `json:"model,omitempty"`
	PromptID        string         `json:"prompt_id,omitempty"`
	ResultPredicate string         `json:"result_predicate,omitempty"`
	ResultActor     string         `json:"result_actor,omitempty"`
}

// promptResultAttrs defines the attribute schema for prompt result attestations.
type promptResultAttrs struct {
	Response      string `attr:"response"`
	SourceID      string `attr:"source_id"`
	Template      string `attr:"template"`
	PromptHandler string `attr:"prompt_handler"`
}

// JobResult represents a single prompt execution result from a job
type JobResult struct {
	SourceAttestationID string `json:"source_attestation_id"`
	Prompt              string `json:"prompt"`
	Response            string `json:"response"`
	ResultAttestationID string `json:"result_attestation_id,omitempty"`
	PromptTokens        int    `json:"prompt_tokens,omitempty"`
	CompletionTokens    int    `json:"completion_tokens,omitempty"`
	TotalTokens         int    `json:"total_tokens,omitempty"`
}

// executePromptJob executes a prompt job asynchronously
func (p *Plugin) executePromptJob(ctx context.Context, jobID string, payload []byte) ([]JobResult, error) {
	logger := p.Services().Logger("openrouter")

	var jobPayload PromptJobPayload
	if err := json.Unmarshal(payload, &jobPayload); err != nil {
		return nil, errors.Wrapf(err, "failed to decode prompt payload for job %s", jobID)
	}

	// Parse frontmatter and template
	doc, err := prompt.ParseFrontmatter(jobPayload.Template)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse frontmatter for job %s", jobID)
	}
	tmpl, err := prompt.Parse(doc.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse template for job %s", jobID)
	}

	// Apply temporal cursor
	filter := jobPayload.AxFilter
	if jobPayload.TemporalCursor != nil && !jobPayload.TemporalCursor.IsZero() {
		filter.TimeStart = jobPayload.TemporalCursor
	}

	// Execute query
	db := p.Services().Database()
	executor := storage.NewExecutor(db)
	result, err := executor.ExecuteAsk(ctx, filter)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to execute ax query for job %s", jobID)
	}

	if len(result.Attestations) == 0 {
		return []JobResult{}, nil
	}

	// Build client
	model := jobPayload.Model
	if model == "" && doc.Metadata.Model != "" {
		model = doc.Metadata.Model
	}

	config := p.Services().Config("openrouter")
	apiKey := config.GetString("api_key")
	if model == "" {
		model = config.GetString("model")
	}
	if model == "" {
		model = DefaultModel
	}

	client := NewClient(Config{
		APIKey:        apiKey,
		Model:         model,
		Logger:        logger,
		DB:            db,
		OperationType: "prompt-execute",
	})

	asStore := p.Services().ATSStore()

	var results []JobResult
	for _, as := range result.Attestations {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}

		interpolated, err := tmpl.Execute(&as)
		if err != nil {
			return results, errors.Wrapf(err, "failed to interpolate template for attestation %s in job %s", as.ID, jobID)
		}

		chatReq := ChatRequest{
			SystemPrompt: jobPayload.SystemPrompt,
			UserPrompt:   interpolated,
		}
		if jobPayload.Model != "" {
			chatReq.Model = &jobPayload.Model
		} else if doc.Metadata.Model != "" {
			chatReq.Model = &doc.Metadata.Model
		}
		if doc.Metadata.Temperature != nil {
			chatReq.Temperature = doc.Metadata.Temperature
		}
		if doc.Metadata.MaxTokens != nil {
			chatReq.MaxTokens = doc.Metadata.MaxTokens
		}

		startTime := time.Now()
		resp, err := client.Chat(ctx, chatReq)
		duration := time.Since(startTime)

		if err != nil {
			return results, errors.Wrapf(err, "LLM call failed for attestation %s in job %s (took %dms)", as.ID, jobID, duration.Milliseconds())
		}

		logger.Infow("LLM call completed",
			"attestation_id", as.ID,
			"duration_ms", duration.Milliseconds(),
			"total_tokens", resp.Usage.TotalTokens,
		)

		// Create result attestation
		resultAs, err := createJobResultAttestation(asStore, &as, resp.Content, jobPayload, model)
		if err != nil {
			return results, errors.Wrapf(err, "failed to create result attestation for %s in job %s", as.ID, jobID)
		}

		results = append(results, JobResult{
			SourceAttestationID: as.ID,
			Prompt:              interpolated,
			Response:            resp.Content,
			ResultAttestationID: resultAs.ID,
			PromptTokens:        resp.Usage.PromptTokens,
			CompletionTokens:    resp.Usage.CompletionTokens,
			TotalTokens:         resp.Usage.TotalTokens,
		})
	}

	return results, nil
}

// createJobResultAttestation creates an attestation from the LLM response
func createJobResultAttestation(
	store interface{ CreateAttestation(*types.As) error },
	sourceAs *types.As,
	response string,
	payload PromptJobPayload,
	model string,
) (*types.As, error) {
	predicate := payload.ResultPredicate
	if predicate == "" {
		predicate = "prompt-result"
	}

	actor := payload.ResultActor
	if actor == "" {
		if payload.PromptID != "" {
			actor = model + "@" + payload.PromptID
		} else {
			actor = model
		}
	}

	asid, err := vanity.GenerateASID(sourceAs.Subjects[0], predicate, sourceAs.ID, actor)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to generate ASID for result of %s", sourceAs.ID)
	}

	now := time.Now()
	resultAs := &types.As{
		ID:         asid,
		Subjects:   sourceAs.Subjects,
		Predicates: []string{predicate},
		Contexts:   []string{sourceAs.ID},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     "prompt",
		Attributes: attrs.From(promptResultAttrs{
			Response:      response,
			SourceID:      sourceAs.ID,
			Template:      payload.Template,
			PromptHandler: fmt.Sprintf("%s (openrouter plugin)", PromptExecuteHandlerName),
		}),
	}

	if err := store.CreateAttestation(resultAs); err != nil {
		return nil, errors.Wrapf(err, "failed to store result attestation %s", asid)
	}

	return resultAs, nil
}
