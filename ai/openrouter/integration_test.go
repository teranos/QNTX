//go:build integration
// +build integration

package openrouter

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration tests that hit the real OpenRouter API
// Run with: go test -tags=integration ./internal/openrouter
// Requires: OPENROUTER_API_KEY environment variable

func TestIntegration_RealAPI(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set, skipping integration tests")
	}

	client := NewClient(Config{
		APIKey:      apiKey,
		Model:       "openai/gpt-3.5-turbo", // Use a cheaper model for testing
		Temperature: 0.1,
		MaxTokens:   50,
		Debug:       true, // Enable debug to see actual API calls
	})

	t.Run("real API call", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := client.Chat(ctx, ChatRequest{
			SystemPrompt: "You are a test assistant. Respond briefly.",
			UserPrompt:   "Say hello in exactly 3 words.",
		})

		if err != nil {
			t.Fatalf("API call failed: %v", err)
		}

		if resp.Content == "" {
			t.Error("expected non-empty response content")
		}

		if resp.Usage.TotalTokens == 0 {
			t.Error("expected non-zero token usage")
		}

		t.Logf("Response: %s", resp.Content)
		t.Logf("Token usage: %d total (%d prompt, %d completion)",
			resp.Usage.TotalTokens, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	})

	t.Run("test configuration options", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Test with custom parameters
		temperature := 0.9
		maxTokens := 20

		resp, err := client.Chat(ctx, ChatRequest{
			UserPrompt:  "Count from 1 to 5",
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
		})

		if err != nil {
			t.Fatalf("API call with custom params failed: %v", err)
		}

		if resp.Content == "" {
			t.Error("expected non-empty response")
		}

		t.Logf("Custom params response: %s", resp.Content)
	})
}

func TestIntegration_ErrorHandling(t *testing.T) {
	t.Run("invalid API key", func(t *testing.T) {
		client := NewClient(Config{
			APIKey: "invalid-key-12345",
			Debug:  true,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := client.Chat(ctx, ChatRequest{
			UserPrompt: "Hello",
		})

		if err == nil {
			t.Fatal("expected error with invalid API key")
		}

		t.Logf("Expected error with invalid key: %v", err)
	})
}
