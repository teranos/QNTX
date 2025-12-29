package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/am"
)

// LocalProvider implements Provider interface for local inference servers
// Supports Ollama, LocalAI, or any OpenAI-compatible local endpoint
type LocalProvider struct {
	baseURL    string
	model      string
	httpClient *http.Client
	config     *am.LocalInferenceConfig
}

// NewLocalProvider creates a provider for local inference
func NewLocalProvider(cfg *am.LocalInferenceConfig) *LocalProvider {
	return &LocalProvider{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
		},
		config: cfg,
	}
}

// ChatCompletionRequest matches OpenAI API format (Ollama is compatible)
type ChatCompletionRequest struct {
	Model    string          `json:"model"`
	Messages []ChatMessage   `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *CompletionOpts `json:"options,omitempty"` // Ollama-specific options
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionOpts struct {
	Temperature float64 `json:"temperature,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
	MaxTokens   int     `json:"num_predict,omitempty"` // Ollama uses num_predict
	NumCtx      int     `json:"num_ctx,omitempty"`     // Context window size (Ollama default: 4096)
}

// ChatCompletionResponse matches OpenAI API format
type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int         `json:"index"`
		Message ChatMessage `json:"message"`
		// Ollama uses "finish_reason", OpenAI uses "finish_reason"
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// GenerateText sends a prompt to local inference server
func (lp *LocalProvider) GenerateText(systemPrompt, userPrompt string) (string, error) {
	// Use background context with client timeout
	ctx := context.Background()
	return lp.generateTextWithContext(ctx, systemPrompt, userPrompt)
}

// generateTextWithContext sends a prompt with context support for cancellation
func (lp *LocalProvider) generateTextWithContext(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	// Configure context size from config (0 = use model default)
	numCtx := 0
	if lp.config.ContextSize > 0 {
		numCtx = lp.config.ContextSize
	}

	reqBody := ChatCompletionRequest{
		Model:    lp.model,
		Messages: messages,
		Stream:   false,
		Options: &CompletionOpts{
			Temperature: 0.7,
			MaxTokens:   4096,
			NumCtx:      numCtx, // Set context window size
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Use OpenAI-compatible endpoint (works for Ollama, LocalAI, etc.)
	endpoint := lp.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := lp.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("local inference returned status %d: %s", resp.StatusCode, string(body))
	}

	var completion ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned")
	}

	return completion.Choices[0].Message.Content, nil
}

// EstimateCost returns zero cost for local inference
// Note: Could track GPU-seconds if monitoring is implemented
func (lp *LocalProvider) EstimateCost(promptTokens, completionTokens int) float64 {
	// Local inference has zero API cost
	// Future: Track GPU resource usage instead
	// See internal/pulse/budget.go TODO(gpu-inference)
	return 0.0
}

// GetModelName returns the configured local model name
func (lp *LocalProvider) GetModelName() string {
	return lp.model
}

// StreamingChunk represents a chunk of streamed response
type StreamingChunk struct {
	Content string
	Done    bool
	Error   error
}

// GenerateTextStreaming sends a prompt and streams the response token by token
// Caller is responsible for closing chunkChan
func (lp *LocalProvider) GenerateTextStreaming(systemPrompt, userPrompt string, chunkChan chan<- StreamingChunk) error {
	// Use background context with client timeout
	ctx := context.Background()
	return lp.generateTextStreamingWithContext(ctx, systemPrompt, userPrompt, chunkChan)
}

// generateTextStreamingWithContext sends a prompt with context support and streams the response
func (lp *LocalProvider) generateTextStreamingWithContext(ctx context.Context, systemPrompt, userPrompt string, chunkChan chan<- StreamingChunk) error {
	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	// Configure context size from config (0 = use model default)
	numCtx := 0
	if lp.config.ContextSize > 0 {
		numCtx = lp.config.ContextSize
	}

	reqBody := ChatCompletionRequest{
		Model:    lp.model,
		Messages: messages,
		Stream:   true, // Enable streaming
		Options: &CompletionOpts{
			Temperature: 0.7,
			MaxTokens:   4096,
			NumCtx:      numCtx,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		chunkChan <- StreamingChunk{Error: fmt.Errorf("failed to marshal request: %w", err)}
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	endpoint := lp.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		chunkChan <- StreamingChunk{Error: fmt.Errorf("failed to create request: %w", err)}
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := lp.httpClient.Do(req)
	if err != nil {
		chunkChan <- StreamingChunk{Error: fmt.Errorf("request failed: %w", err)}
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("local inference returned status %d: %s", resp.StatusCode, string(body))
		chunkChan <- StreamingChunk{Error: err}
		return err
	}

	// Read SSE stream line by line
	// Ollama returns Server-Sent Events format: "data: {...JSON...}\n"
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines
		if line == "" {
			continue
		}

		// SSE format: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		// Extract JSON after "data: " prefix
		jsonData := strings.TrimPrefix(line, "data: ")

		// Handle [DONE] marker
		if jsonData == "[DONE]" {
			chunkChan <- StreamingChunk{Done: true}
			return nil
		}

		// Parse JSON chunk
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
			chunkChan <- StreamingChunk{Error: fmt.Errorf("failed to decode chunk: %w", err)}
			return fmt.Errorf("failed to decode chunk: %w", err)
		}

		if len(chunk.Choices) > 0 {
			content := chunk.Choices[0].Delta.Content
			if content != "" {
				chunkChan <- StreamingChunk{Content: content}
			}

			// Check for completion
			if chunk.Choices[0].FinishReason != "" {
				chunkChan <- StreamingChunk{Done: true}
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		chunkChan <- StreamingChunk{Error: fmt.Errorf("stream read error: %w", err)}
		return fmt.Errorf("stream read error: %w", err)
	}

	// Stream ended without explicit completion
	chunkChan <- StreamingChunk{Done: true}
	return nil
}

// ChatStreaming implements StreamingAIClient interface for Ollama streaming
// This bridges the OpenRouter ChatRequest format to Ollama's streaming API
func (lp *LocalProvider) ChatStreaming(ctx context.Context, req openrouter.ChatRequest, streamChan chan<- StreamChunk) error {
	// Combine system and user prompts
	systemPrompt := req.SystemPrompt
	userPrompt := req.UserPrompt

	// Create internal channel for low-level streaming
	internalChan := make(chan StreamingChunk, 10)

	// Start GenerateTextStreaming in goroutine with context support
	errChan := make(chan error, 1)
	go func() {
		defer close(internalChan)
		err := lp.generateTextStreamingWithContext(ctx, systemPrompt, userPrompt, internalChan)
		errChan <- err
	}()

	// Forward chunks from internal channel to output channel, converting types
	var streamErr error
	for chunk := range internalChan {
		streamChan <- StreamChunk{
			Content: chunk.Content,
			Done:    chunk.Done,
			Error:   chunk.Error,
		}
		if chunk.Error != nil {
			streamErr = chunk.Error
		}
	}

	// Wait for goroutine completion and check for errors
	if err := <-errChan; err != nil {
		return err
	}

	return streamErr
}
