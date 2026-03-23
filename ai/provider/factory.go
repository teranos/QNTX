package provider

import (
	"context"
)

// AIClient interface for LLM inference providers.
// All providers are gRPC plugins (openrouter, llama-cpp, etc.)
type AIClient interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// StreamingAIClient is an optional interface for clients that support streaming.
// Check if a client implements this interface using type assertion.
type StreamingAIClient interface {
	AIClient
	ChatStreaming(ctx context.Context, req ChatRequest, streamChan chan<- StreamChunk) error
}

// StreamChunk represents a chunk of streamed LLM response
type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}
