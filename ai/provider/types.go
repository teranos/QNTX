package provider

// ProviderType represents the type of AI/LLM provider.
// All providers are gRPC plugins.
type ProviderType string

const (
	ProviderTypeOpenRouter ProviderType = "openrouter" // OpenRouter cloud service (gateway to multiple models)
	ProviderTypeScry       ProviderType = "scry"       // Scry — local inference via llama.cpp gRPC plugin
)
