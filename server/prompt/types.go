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
