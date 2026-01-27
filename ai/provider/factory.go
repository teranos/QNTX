package provider

import (
	"context"
	"database/sql"

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
)

// AIClient interface for both OpenRouter and Local inference providers
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

// NewAIClient creates an AI client based on explicit provider selection or configuration
// This is the main factory for LLM inference providers (not video/ONNX processing)
func NewAIClient(cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	// Determine which provider to use
	provider := DetermineProvider(cfg, "")
	return NewAIClientForProvider(provider, cfg, db, verbosity, operationType, entityType, entityID)
}

// NewAIClientWithProvider creates an AI client for a specific provider
// Used when provider is explicitly specified (e.g., in prompt frontmatter)
func NewAIClientWithProvider(providerName string, cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	provider := ProviderType(providerName)
	return NewAIClientForProvider(provider, cfg, db, verbosity, operationType, entityType, entityID)
}

// NewAIClientForProvider creates the appropriate client for the given provider type
func NewAIClientForProvider(provider ProviderType, cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	return NewAIClientForProviderWithModel(provider, cfg, "", db, verbosity, operationType, entityType, entityID)
}

// NewAIClientForProviderWithModel creates the appropriate client with an optional model override
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

	case ProviderTypeOpenRouter:
		model := modelOverride
		if model == "" {
			model = cfg.OpenRouter.Model
		}
		return openrouter.NewClient(openrouter.Config{
			APIKey:        cfg.OpenRouter.APIKey,
			Model:         model,
			DB:            db,
			Verbosity:     verbosity,
			OperationType: operationType,
			EntityType:    entityType,
			EntityID:      entityID,
		})

	default:
		// Fallback to OpenRouter for unknown providers
		model := modelOverride
		if model == "" {
			model = cfg.OpenRouter.Model
		}
		return openrouter.NewClient(openrouter.Config{
			APIKey:        cfg.OpenRouter.APIKey,
			Model:         model,
			DB:            db,
			Verbosity:     verbosity,
			OperationType: operationType,
			EntityType:    entityType,
			EntityID:      entityID,
		})
	}
}

// DetermineProvider determines which provider to use based on configuration and overrides
func DetermineProvider(cfg *am.Config, explicitProvider string) ProviderType {
	// If explicitly specified, use that
	if explicitProvider != "" {
		return ProviderType(explicitProvider)
	}

	// Build ProviderConfig from current am.Config
	// This uses the proper abstraction while maintaining the same behavior
	pc := &ProviderConfig{
		DefaultProvider: ProviderTypeOpenRouter,
		ProviderPriority: []ProviderType{
			ProviderTypeLocal,      // Check local first if enabled
			ProviderTypeOpenRouter, // Fallback to OpenRouter
		},
		Providers: map[ProviderType]bool{
			ProviderTypeLocal:      cfg.LocalInference.Enabled && cfg.LocalInference.BaseURL != "",
			ProviderTypeOpenRouter: true, // Always available as fallback
			// Future providers will be added here:
			// ProviderTypeLlamaCpp: cfg.LlamaCpp.Enabled && cfg.LlamaCpp.ServerURL != "",
		},
	}

	return pc.GetActiveProvider()
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
	result, err := lca.provider.GenerateTextWithUsage(ctx, req.SystemPrompt, req.UserPrompt)
	if err != nil {
		return nil, errors.Wrap(err, "local provider text generation failed")
	}

	return &openrouter.ChatResponse{
		Content: result.Content,
		Usage: openrouter.Usage{
			PromptTokens:     result.PromptTokens,
			CompletionTokens: result.CompletionTokens,
			TotalTokens:      result.TotalTokens,
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
			return errors.Wrap(ctx.Err(), "streaming cancelled by context")
		case chunk, ok := <-providerChan:
			if !ok {
				// Channel closed, streaming complete
				if err := <-errChan; err != nil {
					return errors.Wrap(err, "local provider streaming failed")
				}
				return nil
			}
			// Convert provider chunk to AI client chunk
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
var _ AIClient = (*openrouter.Client)(nil)
var _ AIClient = (*LocalClientAdapter)(nil)
var _ StreamingAIClient = (*LocalClientAdapter)(nil)
