package prompt

// Constants for prompt API defaults
const (
	// DefaultAxQueryLimit is the default query limit when parsing ax queries without explicit limit
	DefaultAxQueryLimit = 100
	// DefaultLocalModel is the default model for local inference when not configured
	DefaultLocalModel = "llama3.2:3b"
	// DefaultOpenRouterModel is the default model for OpenRouter when not configured
	DefaultOpenRouterModel = "openai/gpt-4o-mini"
)

// PreviewRequest represents a request to preview prompt execution with X-sampling
type PreviewRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`                   // Prompt template with {{field}} placeholders
	SystemPrompt string `json:"system_prompt,omitempty"`     // Optional system instruction for the LLM
	SampleSize   int    `json:"sample_size,omitempty"`       // X value: number of samples to test (default: 1)
	Provider     string `json:"provider,omitempty"`          // "openrouter" or "local"
	Model        string `json:"model,omitempty"`              // Model override
	PromptID     string `json:"prompt_id,omitempty"`         // Optional prompt ID for tracking
	PromptVersion int   `json:"prompt_version,omitempty"`    // Optional prompt version for comparison
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

// PreviewResponse represents the preview response with X samples
type PreviewResponse struct {
	TotalAttestations int             `json:"total_attestations"`   // Total matching attestations from ax query
	SampleSize        int             `json:"sample_size"`          // X value used for sampling
	Samples           []PreviewSample `json:"samples"`              // X sample execution results
	SuccessCount      int             `json:"success_count"`        // Number of successful samples
	FailureCount      int             `json:"failure_count"`        // Number of failed samples
	Error             string          `json:"error,omitempty"`      // Global error if any
}

// ExecuteRequest represents a request to execute a prompt
type ExecuteRequest struct {
	AxQuery      string `json:"ax_query"`
	Template     string `json:"template"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Provider     string `json:"provider,omitempty"` // "openrouter" or "local"
	Model        string `json:"model,omitempty"`
}

// ExecuteResult represents the output of a prompt execution
type ExecuteResult struct {
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

// ExecuteResponse represents the execution response
type ExecuteResponse struct {
	Results          []ExecuteResult `json:"results"`
	AttestationCount int             `json:"attestation_count"`
	Error            string          `json:"error,omitempty"`
}

// SaveRequest represents a prompt save/update request
type SaveRequest struct {
	ID           string `json:"id,omitempty"`     // Optional: for updates
	Name         string `json:"name"`             // Unique identifier
	Template     string `json:"template"`         // Prompt template
	SystemPrompt string `json:"system_prompt,omitempty"`
	AxPattern    string `json:"ax_pattern,omitempty"` // Default ax query pattern
	Provider     string `json:"provider,omitempty"`   // Default provider
	Model        string `json:"model,omitempty"`      // Default model
}
