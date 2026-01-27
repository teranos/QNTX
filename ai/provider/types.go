package provider

// ProviderType represents the type of AI/LLM provider
// These are for text generation, not video/image processing (which uses ONNX)
type ProviderType string

const (
	// Current providers
	ProviderTypeLocal      ProviderType = "local"      // Ollama, LocalAI, or any OpenAI-compatible local server
	ProviderTypeOpenRouter ProviderType = "openrouter" // OpenRouter cloud service (gateway to multiple models)

	// Future provider options (ready to implement):
	ProviderTypeAnthropic  ProviderType = "anthropic"  // Direct Anthropic API (Claude)
	ProviderTypeOpenAI     ProviderType = "openai"     // Direct OpenAI API (GPT-4, etc.)
	ProviderTypeAzure      ProviderType = "azure"      // Azure OpenAI Service
	ProviderTypeCohere     ProviderType = "cohere"     // Cohere models
	ProviderTypeHuggingFace ProviderType = "huggingface" // HuggingFace Inference API
	ProviderTypeReplicate  ProviderType = "replicate"  // Replicate.com models
	ProviderTypeVertex     ProviderType = "vertex"     // Google Cloud Vertex AI
	ProviderTypeBedrock    ProviderType = "bedrock"    // AWS Bedrock
)

// ProviderConfig represents configuration for selecting providers
type ProviderConfig struct {
	// Default provider to use when not specified
	DefaultProvider ProviderType

	// Priority order for fallback (if primary provider fails)
	ProviderPriority []ProviderType

	// Per-provider enable flags
	Providers map[ProviderType]bool
}

// IsProviderEnabled checks if a specific provider is enabled
func (pc *ProviderConfig) IsProviderEnabled(provider ProviderType) bool {
	if pc.Providers == nil {
		return false
	}
	return pc.Providers[provider]
}

// GetActiveProvider returns the provider to use based on configuration
// Returns the first enabled provider in priority order
func (pc *ProviderConfig) GetActiveProvider() ProviderType {
	// If default is enabled, use it
	if pc.IsProviderEnabled(pc.DefaultProvider) {
		return pc.DefaultProvider
	}

	// Otherwise check priority order
	for _, provider := range pc.ProviderPriority {
		if pc.IsProviderEnabled(provider) {
			return provider
		}
	}

	// Fallback to OpenRouter if nothing else is configured
	return ProviderTypeOpenRouter
}