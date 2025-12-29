package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
