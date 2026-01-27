package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/am"
)

// TestChatStreaming_ContextCancellation tests that streaming cleanup works properly
// when context is cancelled mid-stream (Issue #5 from PR review)
func TestChatStreaming_ContextCancellation(t *testing.T) {
	// Create mock server that streams slowly to allow cancellation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter doesn't support flushing")
		}

		// Send a few chunks with delays to allow cancellation
		for i := 0; i < 10; i++ {
			// Check if client disconnected
			select {
			case <-r.Context().Done():
				return
			default:
			}

			// Send chunk in Ollama's streaming format
			chunk := `{"message":{"role":"assistant","content":"chunk"},"done":false}`
			w.Write([]byte(chunk + "\n"))
			flusher.Flush()

			// Delay between chunks
			time.Sleep(50 * time.Millisecond)
		}

		// Send final chunk
		finalChunk := `{"message":{"role":"assistant","content":""},"done":true}`
		w.Write([]byte(finalChunk + "\n"))
		flusher.Flush()
	}))
	defer server.Close()

	// Create local provider with test server
	provider := NewLocalProvider(&am.LocalInferenceConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		TimeoutSeconds: 30,
	})

	// Create context that will be cancelled mid-stream
	ctx, cancel := context.WithCancel(context.Background())

	// Create output channel
	streamChan := make(chan StreamingChunk, 10)

	// Start streaming in goroutine
	errChan := make(chan error, 1)
	go func() {
		err := provider.generateTextStreamingWithContext(ctx, "system", "user", streamChan)
		close(streamChan)
		errChan <- err
	}()

	// Receive a few chunks then cancel
	chunksReceived := 0
	for chunk := range streamChan {
		chunksReceived++
		if chunksReceived >= 2 {
			cancel() // Cancel after receiving 2 chunks
			break
		}
		if chunk.Error != nil {
			t.Fatalf("Unexpected error in chunk: %v", chunk.Error)
		}
	}

	// Wait for goroutine to finish
	err := <-errChan

	// The main goal is to verify no double-close panic occurs during cancellation
	// Error behavior may vary depending on when cancellation happens
	if err != nil {
		t.Logf("Got error after cancellation (expected): %v", err)
	}

	// If we got here without panic, the test passes
	t.Logf("Successfully cancelled after %d chunks without panic", chunksReceived)
}

// TestChatStreaming_ChannelCleanup tests that channels are properly closed
func TestChatStreaming_ChannelCleanup(t *testing.T) {
	// Create mock server that completes successfully
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, _ := w.(http.Flusher)

		// Send single chunk
		chunk := `{"message":{"role":"assistant","content":"test"},"done":false}`
		w.Write([]byte(chunk + "\n"))
		flusher.Flush()

		// Send done marker
		finalChunk := `{"message":{"role":"assistant","content":""},"done":true}`
		w.Write([]byte(finalChunk + "\n"))
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewLocalProvider(&am.LocalInferenceConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		TimeoutSeconds: 5,
	})

	ctx := context.Background()
	streamChan := make(chan StreamingChunk, 10)

	// Run streaming
	errChan := make(chan error, 1)
	go func() {
		err := provider.generateTextStreamingWithContext(ctx, "system", "user", streamChan)
		close(streamChan) // Caller closes channel
		errChan <- err
	}()

	// Consume all chunks
	for range streamChan {
		// Just drain the channel
	}

	// Check error
	err := <-errChan
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// If we got here without panic, channels were closed properly
	t.Log("Channel cleanup completed without panic")
}

// TestGenerateTextWithUsage_ReturnsTokenStats verifies that token usage
// statistics from Ollama's OpenAI-compatible API are propagated through
// the LocalProvider and LocalClientAdapter to the caller.
func TestGenerateTextWithUsage_ReturnsTokenStats(t *testing.T) {
	// Create mock server that returns OpenAI-compatible response with usage stats
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("Expected path /v1/chat/completions, got %s", r.URL.Path)
		}

		// Return OpenAI-compatible response with token usage
		response := ChatCompletionResponse{
			ID:      "test-id",
			Object:  "chat.completion",
			Created: 1234567890,
			Model:   "test-model",
			Choices: []struct {
				Index   int         `json:"index"`
				Message ChatMessage `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Index:        0,
					Message:      ChatMessage{Role: "assistant", Content: "Hello, world!"},
					FinishReason: "stop",
				},
			},
			Usage: &struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{
				PromptTokens:     42,
				CompletionTokens: 17,
				TotalTokens:      59,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create local provider with test server
	provider := NewLocalProvider(&am.LocalInferenceConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		TimeoutSeconds: 5,
	})

	// Call GenerateTextWithUsage
	result, err := provider.GenerateTextWithUsage(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("GenerateTextWithUsage failed: %v", err)
	}

	// Verify content
	if result.Content != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", result.Content)
	}

	// Verify token stats are propagated
	if result.PromptTokens != 42 {
		t.Errorf("Expected PromptTokens=42, got %d", result.PromptTokens)
	}
	if result.CompletionTokens != 17 {
		t.Errorf("Expected CompletionTokens=17, got %d", result.CompletionTokens)
	}
	if result.TotalTokens != 59 {
		t.Errorf("Expected TotalTokens=59, got %d", result.TotalTokens)
	}
}

// TestLocalClientAdapter_PropagatesTokenStats verifies the adapter passes
// token stats through to the openrouter.ChatResponse format.
func TestLocalClientAdapter_PropagatesTokenStats(t *testing.T) {
	// Create mock server with token usage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ChatCompletionResponse{
			ID:    "test-id",
			Model: "test-model",
			Choices: []struct {
				Index   int         `json:"index"`
				Message ChatMessage `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Index:        0,
					Message:      ChatMessage{Role: "assistant", Content: "Test response"},
					FinishReason: "stop",
				},
			},
			Usage: &struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create adapter via factory
	adapter := NewLocalClient(LocalClientConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		TimeoutSeconds: 5,
	})

	// Call Chat through the adapter
	resp, err := adapter.Chat(context.Background(), openrouter.ChatRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify token stats in response
	if resp.Usage.PromptTokens != 100 {
		t.Errorf("Expected PromptTokens=100, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 50 {
		t.Errorf("Expected CompletionTokens=50, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 150 {
		t.Errorf("Expected TotalTokens=150, got %d", resp.Usage.TotalTokens)
	}
}
