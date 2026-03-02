package provider

import (
	"context"
	"database/sql"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
)

// AIClient interface for LLM inference providers.
// OpenRouter has been moved to the qntx-openrouter gRPC plugin.
// The core factory now only supports local inference (Ollama, LocalAI, etc.)
type AIClient interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// StreamingAIClient is an optional interface for clients that support streaming
// Check if a client implements this interface using type assertion
type StreamingAIClient interface {
	AIClient
	// ChatStreaming sends a request and streams the response token by token
	// The stream channel receives chunks as they arrive and is closed when done
	ChatStreaming(ctx context.Context, req ChatRequest, streamChan chan<- StreamChunk) error
}

// StreamChunk represents a chunk of streamed LLM response
type StreamChunk struct {
	Content string // Token/chunk of text
	Done    bool   // True when stream is complete
	Error   error  // Error if streaming failed
}

// NewAIClient creates an AI client based on configuration.
// Only local inference is supported in core. For OpenRouter, use the qntx-openrouter plugin.
func NewAIClient(cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	provider := DetermineProvider(cfg, "")
	return NewAIClientForProvider(provider, cfg, db, verbosity, operationType, entityType, entityID)
}

// NewAIClientWithProvider creates an AI client for a specific provider
func NewAIClientWithProvider(providerName string, cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	provider := ProviderType(providerName)
	return NewAIClientForProvider(provider, cfg, db, verbosity, operationType, entityType, entityID)
}

// NewAIClientForProvider creates the appropriate client for the given provider type
func NewAIClientForProvider(provider ProviderType, cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	return NewAIClientForProviderWithModel(provider, cfg, "", db, verbosity, operationType, entityType, entityID)
}

// NewAIClientForProviderWithModel creates the appropriate client with an optional model override.
// Only local inference is available in core. OpenRouter moved to qntx-openrouter plugin.
func NewAIClientForProviderWithModel(provider ProviderType, cfg *am.Config, modelOverride string, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	switch provider {
	case ProviderTypeLocal:
		model := modelOverride
		if model == "" {
			model = cfg.LocalInference.Model
		}
		return NewLocalClient(LocalClientConfig{
			BaseURL:        cfg.LocalInference.BaseURL,
			Model:          model,
			TimeoutSeconds: cfg.LocalInference.TimeoutSeconds,
			DB:             db,
			Verbosity:      verbosity,
			OperationType:  operationType,
			EntityType:     entityType,
			EntityID:       entityID,
		})

	default:
		// Local is the only in-process provider. OpenRouter runs as gRPC plugin.
		model := modelOverride
		if model == "" {
			model = cfg.LocalInference.Model
		}
		return NewLocalClient(LocalClientConfig{
			BaseURL:        cfg.LocalInference.BaseURL,
			Model:          model,
			TimeoutSeconds: cfg.LocalInference.TimeoutSeconds,
			DB:             db,
			Verbosity:      verbosity,
			OperationType:  operationType,
			EntityType:     entityType,
			EntityID:       entityID,
		})
	}
}

// DetermineProvider determines which provider to use based on configuration and overrides.
// With OpenRouter moved to a plugin, the core only supports local inference.
func DetermineProvider(cfg *am.Config, explicitProvider string) ProviderType {
	if explicitProvider != "" {
		return ProviderType(explicitProvider)
	}

	// Local inference is the only in-process provider
	if cfg.LocalInference.Enabled && cfg.LocalInference.BaseURL != "" {
		return ProviderTypeLocal
	}

	// Default to local — OpenRouter is handled by the qntx-openrouter plugin
	return ProviderTypeLocal
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

// NewLocalClient creates a client that wraps LocalProvider to implement AIClient
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

// LocalClientAdapter adapts LocalProvider to match AIClient interface
type LocalClientAdapter struct {
	provider *LocalProvider
	config   LocalClientConfig
}

// Chat implements the AIClient interface for local inference
func (lca *LocalClientAdapter) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	result, err := lca.provider.GenerateTextWithUsage(ctx, req.SystemPrompt, req.UserPrompt)
	if err != nil {
		return nil, errors.Wrap(err, "local provider text generation failed")
	}

	return &ChatResponse{
		Content: result.Content,
		Usage: Usage{
			PromptTokens:     result.PromptTokens,
			CompletionTokens: result.CompletionTokens,
			TotalTokens:      result.TotalTokens,
		},
	}, nil
}

// ChatStreaming implements the StreamingAIClient interface for local inference
func (lca *LocalClientAdapter) ChatStreaming(ctx context.Context, req ChatRequest, streamChan chan<- StreamChunk) error {
	providerChan := make(chan StreamingChunk, 10)

	errChan := make(chan error, 1)
	go func() {
		err := lca.provider.GenerateTextStreaming(req.SystemPrompt, req.UserPrompt, providerChan)
		errChan <- err
	}()

	for {
		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), "streaming cancelled by context")
		case chunk, ok := <-providerChan:
			if !ok {
				if err := <-errChan; err != nil {
					return errors.Wrap(err, "local provider streaming failed")
				}
				return nil
			}
			streamChan <- StreamChunk{
				Content: chunk.Content,
				Done:    chunk.Done,
				Error:   chunk.Error,
			}
			if chunk.Done || chunk.Error != nil {
				if chunk.Error != nil {
					return errors.Wrap(chunk.Error, "streaming chunk error")
				}
				return nil
			}
		}
	}
}

// Verify interfaces are implemented
var _ AIClient = (*LocalClientAdapter)(nil)
var _ StreamingAIClient = (*LocalClientAdapter)(nil)
