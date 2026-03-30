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
	Signal  *TokenSignal // Per-token signal data (nil if unavailable)
	// Final chunk fields
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// TokenSignal carries per-token inference signal data
type TokenSignal struct {
	Confidence       float32
	Entropy          float32
	TopGap           float32
	TopK             []TokenCandidate
	FullDistribution []float32
	SamplerStages    []SamplerStageSignal
}

// TokenCandidate is a candidate token from the top-k distribution
type TokenCandidate struct {
	ID   int32
	Text string
	Prob float32
}

// SamplerStageSignal is a snapshot of the token distribution after a sampler stage
type SamplerStageSignal struct {
	Name        string
	ActiveCount int32
	Top1Prob    float32
	Entropy     float32
	TopK        []TokenCandidate
}
