package provider

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/teranos/QNTX/ai/anthropic"
	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/am"
)

// Provider represents an LLM provider type
type Provider string

const (
	// ProviderLocal uses local inference (Ollama, LocalAI)
	ProviderLocal Provider = "local"
	// ProviderOpenRouter uses OpenRouter.ai API
	ProviderOpenRouter Provider = "openrouter"
	// ProviderAnthropic uses direct Anthropic API
	ProviderAnthropic Provider = "anthropic"
	// ProviderAuto automatically selects based on configuration
	ProviderAuto Provider = "auto"
)

// AIClient interface for all LLM providers
// Ensures compatibility between different providers
type AIClient interface {
	Chat(ctx context.Context, req openrouter.ChatRequest) (*openrouter.ChatResponse, error)
}

// StreamingAIClient is an optional interface for clients that support streaming
// Check if a client implements this interface using type assertion
type StreamingAIClient interface {
	AIClient
	// ChatStreaming sends a request and streams the response token by token
	// The stream channel receives chunks as they arrive and is closed when done
	ChatStreaming(ctx context.Context, req openrouter.ChatRequest, streamChan chan<- StreamChunk) error
}

// StreamChunk represents a chunk of streamed LLM response
type StreamChunk struct {
	Content string // Token/chunk of text
	Done    bool   // True when stream is complete
	Error   error  // Error if streaming failed
}

// ClientConfig holds common configuration for creating AI clients
type ClientConfig struct {
	DB            *sql.DB
	Verbosity     int
	OperationType string
	EntityType    string
	EntityID      string
}

// NewAIClient creates an AI client based on configuration (auto-selection)
// Priority: LocalInference (if enabled) → Anthropic (if API key set) → OpenRouter
// This factory function centralizes provider selection logic
func NewAIClient(cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	clientCfg := ClientConfig{
		DB:            db,
		Verbosity:     verbosity,
		OperationType: operationType,
		EntityType:    entityType,
		EntityID:      entityID,
	}
	return NewAIClientWithProvider(cfg, ProviderAuto, clientCfg)
}

// NewAIClientWithProvider creates an AI client for a specific provider
// Use ProviderAuto to let the factory decide based on configuration
func NewAIClientWithProvider(cfg *am.Config, provider Provider, clientCfg ClientConfig) AIClient {
	switch provider {
	case ProviderLocal:
		return newLocalClient(cfg, clientCfg)
	case ProviderAnthropic:
		return newAnthropicClient(cfg, clientCfg)
	case ProviderOpenRouter:
		return newOpenRouterClient(cfg, clientCfg)
	case ProviderAuto:
		return autoSelectClient(cfg, clientCfg)
	default:
		// Unknown provider, fall back to auto
		return autoSelectClient(cfg, clientCfg)
	}
}

// autoSelectClient automatically selects the best available provider
// Priority: LocalInference (if enabled) → Anthropic (if API key set) → OpenRouter
func autoSelectClient(cfg *am.Config, clientCfg ClientConfig) AIClient {
	// Priority 1: Local inference if enabled
	if cfg.LocalInference.Enabled {
		return newLocalClient(cfg, clientCfg)
	}

	// Priority 2: Anthropic if API key is set
	if cfg.Anthropic.APIKey != "" {
		return newAnthropicClient(cfg, clientCfg)
	}

	// Priority 3: OpenRouter (default)
	return newOpenRouterClient(cfg, clientCfg)
}

// newLocalClient creates a local inference client
func newLocalClient(cfg *am.Config, clientCfg ClientConfig) AIClient {
	return NewLocalClient(LocalClientConfig{
		BaseURL:        cfg.LocalInference.BaseURL,
		Model:          cfg.LocalInference.Model,
		TimeoutSeconds: cfg.LocalInference.TimeoutSeconds,
		DB:             clientCfg.DB,
		Verbosity:      clientCfg.Verbosity,
		OperationType:  clientCfg.OperationType,
		EntityType:     clientCfg.EntityType,
		EntityID:       clientCfg.EntityID,
	})
}

// newAnthropicClient creates an Anthropic API client wrapped in an adapter
// The adapter provides StreamingAIClient compatibility
func newAnthropicClient(cfg *am.Config, clientCfg ClientConfig) AIClient {
	client := anthropic.NewClient(anthropic.Config{
		APIKey:        cfg.Anthropic.APIKey,
		Model:         cfg.Anthropic.Model,
		Temperature:   cfg.Anthropic.Temperature,
		MaxTokens:     cfg.Anthropic.MaxTokens,
		DB:            clientCfg.DB,
		Verbosity:     clientCfg.Verbosity,
		OperationType: clientCfg.OperationType,
		EntityType:    clientCfg.EntityType,
		EntityID:      clientCfg.EntityID,
	})
	return &AnthropicAdapter{client: client}
}

// newOpenRouterClient creates an OpenRouter API client
func newOpenRouterClient(cfg *am.Config, clientCfg ClientConfig) AIClient {
	return openrouter.NewClient(openrouter.Config{
		APIKey:        cfg.OpenRouter.APIKey,
		Model:         cfg.OpenRouter.Model,
		Temperature:   cfg.OpenRouter.Temperature,
		MaxTokens:     cfg.OpenRouter.MaxTokens,
		DB:            clientCfg.DB,
		Verbosity:     clientCfg.Verbosity,
		OperationType: clientCfg.OperationType,
		EntityType:    clientCfg.EntityType,
		EntityID:      clientCfg.EntityID,
	})
}

// GetAvailableProviders returns a list of configured/available providers
func GetAvailableProviders(cfg *am.Config) []Provider {
	var providers []Provider

	// Local is always "available" if enabled
	if cfg.LocalInference.Enabled {
		providers = append(providers, ProviderLocal)
	}

	// Anthropic available if API key is set
	if cfg.Anthropic.APIKey != "" {
		providers = append(providers, ProviderAnthropic)
	}

	// OpenRouter available if API key is set
	if cfg.OpenRouter.APIKey != "" {
		providers = append(providers, ProviderOpenRouter)
	}

	return providers
}

// ParseProvider converts a string to a Provider type
func ParseProvider(s string) (Provider, error) {
	switch s {
	case "local", "ollama", "localai":
		return ProviderLocal, nil
	case "openrouter", "or":
		return ProviderOpenRouter, nil
	case "anthropic", "claude":
		return ProviderAnthropic, nil
	case "auto", "":
		return ProviderAuto, nil
	default:
		return "", fmt.Errorf("unknown provider: %s (valid: local, openrouter, anthropic, auto)", s)
	}
}

// LocalClientConfig holds configuration for local inference client
type LocalClientConfig struct {
	BaseURL        string
	Model          string
	TimeoutSeconds int
	DB             *sql.DB
	Verbosity      int
	OperationType  string
	EntityType     string
	EntityID       string
}

// NewLocalClient creates a client that wraps LocalProvider to be compatible with openrouter.Client interface
func NewLocalClient(cfg LocalClientConfig) AIClient {
	return &LocalClientAdapter{
		provider: NewLocalProvider(&am.LocalInferenceConfig{
			Enabled:        true,
			BaseURL:        cfg.BaseURL,
			Model:          cfg.Model,
			TimeoutSeconds: cfg.TimeoutSeconds,
		}),
		config: cfg,
	}
}

// LocalClientAdapter adapts LocalProvider to match openrouter.Client interface
type LocalClientAdapter struct {
	provider *LocalProvider
	config   LocalClientConfig
}

// Chat implements the AIClient interface for local inference
func (lca *LocalClientAdapter) Chat(ctx context.Context, req openrouter.ChatRequest) (*openrouter.ChatResponse, error) {
	// Note: Model override from req.Model not currently supported for local inference
	// The model is configured in LocalInferenceConfig and used by the provider

	// For now, use simple system+user prompt pattern
	// Future: Support multi-turn conversations if needed
	content, err := lca.provider.GenerateText(ctx, req.SystemPrompt, req.UserPrompt)
	if err != nil {
		return nil, err
	}

	// Return response in openrouter.ChatResponse format
	// Note: Local inference doesn't provide token usage stats by default
	// Future: Parse from Ollama /api/generate response or estimate tokens
	return &openrouter.ChatResponse{
		Content: content,
		Usage: openrouter.Usage{
			PromptTokens:     0, // TODO(QNTX #68): Estimate or get from provider
			CompletionTokens: 0, // TODO(QNTX #68): Estimate or get from provider
			TotalTokens:      0,
		},
	}, nil
}

// ChatStreaming implements the StreamingAIClient interface for local inference
func (lca *LocalClientAdapter) ChatStreaming(ctx context.Context, req openrouter.ChatRequest, streamChan chan<- StreamChunk) error {
	// Create a channel to receive chunks from the provider
	providerChan := make(chan StreamingChunk, 10)

	// Start streaming in a goroutine
	errChan := make(chan error, 1)
	go func() {
		err := lca.provider.GenerateTextStreaming(req.SystemPrompt, req.UserPrompt, providerChan)
		errChan <- err
	}()

	// Forward chunks from provider to caller
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-providerChan:
			if !ok {
				// Channel closed, streaming complete
				err := <-errChan
				return err
			}
			// Convert provider chunk to AI client chunk
			streamChan <- StreamChunk{
				Content: chunk.Content,
				Done:    chunk.Done,
				Error:   chunk.Error,
			}
			if chunk.Done || chunk.Error != nil {
				return chunk.Error
			}
		}
	}
}

// AnthropicAdapter wraps an Anthropic client to implement StreamingAIClient
// This adapter converts between anthropic.StreamChunk and provider.StreamChunk
type AnthropicAdapter struct {
	client *anthropic.Client
}

// Chat delegates to the underlying Anthropic client
func (a *AnthropicAdapter) Chat(ctx context.Context, req openrouter.ChatRequest) (*openrouter.ChatResponse, error) {
	return a.client.Chat(ctx, req)
}

// ChatStreaming implements StreamingAIClient by converting stream chunk types
func (a *AnthropicAdapter) ChatStreaming(ctx context.Context, req openrouter.ChatRequest, streamChan chan<- StreamChunk) error {
	// Create internal channel for anthropic chunks
	anthropicChan := make(chan anthropic.StreamChunk, 10)

	// Start streaming in goroutine
	errChan := make(chan error, 1)
	go func() {
		err := a.client.ChatStreaming(ctx, req, anthropicChan)
		errChan <- err
	}()

	// Convert and forward chunks
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-anthropicChan:
			if !ok {
				return <-errChan
			}
			streamChan <- StreamChunk{
				Content: chunk.Content,
				Done:    chunk.Done,
				Error:   chunk.Error,
			}
			if chunk.Done || chunk.Error != nil {
				return chunk.Error
			}
		}
	}
}

// Verify interfaces are implemented
var _ AIClient = (*openrouter.Client)(nil)
var _ AIClient = (*LocalClientAdapter)(nil)
var _ AIClient = (*anthropic.Client)(nil)
var _ AIClient = (*AnthropicAdapter)(nil)
var _ StreamingAIClient = (*LocalClientAdapter)(nil)
var _ StreamingAIClient = (*AnthropicAdapter)(nil)
