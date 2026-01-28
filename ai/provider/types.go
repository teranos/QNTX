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
// Priority order is used to determine which provider to use
func (pc *ProviderConfig) GetActiveProvider() ProviderType {
	// Priority list determines the order of preference
	// This is the main logic - check providers in priority order
	for _, provider := range pc.ProviderPriority {
		if pc.IsProviderEnabled(provider) {
			return provider
		}
	}

	// If nothing in priority list is enabled, check the default
	// (only if it's not already in the priority list)
	if pc.DefaultProvider != "" && pc.IsProviderEnabled(pc.DefaultProvider) {
		// Check if default is already in priority list
		inPriority := false
		for _, p := range pc.ProviderPriority {
			if p == pc.DefaultProvider {
				inPriority = true
				break
			}
		}
		if !inPriority {
			return pc.DefaultProvider
		}
	}

	// Fallback to OpenRouter if nothing else is configured
	return ProviderTypeOpenRouter
}