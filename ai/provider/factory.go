package provider

import (
	"context"
	"database/sql"

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/am"
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

// NewAIClient creates either an OpenRouter or Local inference client based on configuration
// This factory function centralizes provider selection logic
func NewAIClient(cfg *am.Config, db *sql.DB, verbosity int, operationType, entityType, entityID string) AIClient {
	// Check if local inference is enabled
	if cfg.LocalInference.Enabled {
		// Use local inference (Ollama, LocalAI, etc.)
		return NewLocalClient(LocalClientConfig{
			BaseURL:        cfg.LocalInference.BaseURL,
			Model:          cfg.LocalInference.Model,
			TimeoutSeconds: cfg.LocalInference.TimeoutSeconds,
			DB:             db,
			Verbosity:      verbosity,
			OperationType:  operationType,
			EntityType:     entityType,
			EntityID:       entityID,
		})
	}

	// Default to OpenRouter
	return openrouter.NewClient(openrouter.Config{
		APIKey:        cfg.OpenRouter.APIKey,
		Model:         cfg.OpenRouter.Model,
		DB:            db,
		Verbosity:     verbosity,
		OperationType: operationType,
		EntityType:    entityType,
		EntityID:      entityID,
	})
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
				return <-errChan
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

// Verify interfaces are implemented
var _ AIClient = (*openrouter.Client)(nil)
var _ AIClient = (*LocalClientAdapter)(nil)
var _ StreamingAIClient = (*LocalClientAdapter)(nil)
