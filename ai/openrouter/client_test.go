package openrouter

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestClient_Configuration tests client configuration and defaults
func TestClient_Configuration(t *testing.T) {
	t.Run("applies default values", func(t *testing.T) {
		client := NewClient(Config{
			APIKey: "test-key",
		})

		if client.config.Model != "openai/gpt-4o-mini" {
			t.Errorf("expected default model 'openai/gpt-4o-mini', got %s", client.config.Model)
		}
		if client.config.Temperature == nil || *client.config.Temperature != 0.2 {
			t.Errorf("expected default temperature 0.2, got %v", client.config.Temperature)
		}
		if client.config.MaxTokens == nil || *client.config.MaxTokens != 1000 {
			t.Errorf("expected default max tokens 1000, got %v", client.config.MaxTokens)
		}
	})

	t.Run("preserves custom values", func(t *testing.T) {
		temp := 0.8
		tokens := 2000
		client := NewClient(Config{
			APIKey:      "test-key",
			Model:       "custom/model",
			Temperature: &temp,
			MaxTokens:   &tokens,
			Debug:       true,
		})

		if client.config.Model != "custom/model" {
			t.Errorf("expected custom model, got %s", client.config.Model)
		}
		if *client.config.Temperature != 0.8 {
			t.Errorf("expected custom temperature, got %f", *client.config.Temperature)
		}
		if *client.config.MaxTokens != 2000 {
			t.Errorf("expected custom max tokens, got %d", *client.config.MaxTokens)
		}
		if !client.config.Debug {
			t.Error("expected debug to be true")
		}
	})

	t.Run("backward compatibility constructor", func(t *testing.T) {
		client := NewClientWithAPIKey("test-key")
		if client.config.APIKey != "test-key" {
			t.Errorf("expected API key to be set")
		}
		if client.config.Model != "openai/gpt-4o-mini" {
			t.Error("expected default model to be applied")
		}
	})
}

// TestClient_IsConfigured tests API key validation
func TestClient_IsConfigured(t *testing.T) {
	t.Run("returns true with API key", func(t *testing.T) {
		client := NewClient(Config{APIKey: "test-key"})
		if !client.IsConfigured() {
			t.Error("expected IsConfigured to return true")
		}
	})

	t.Run("returns false without API key", func(t *testing.T) {
		client := NewClient(Config{})
		if client.IsConfigured() {
			t.Error("expected IsConfigured to return false")
		}
	})
}

// TestClient_Chat tests the high-level Chat method
func TestClient_Chat(t *testing.T) {
	t.Run("successful request", func(t *testing.T) {
		// Create mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("expected authorization header")
			}

			// Send mock response
			response := ChatCompletionResponse{
				ID:      "test-id",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   "test-model",
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: "Test response content",
						},
						FinishReason: "stop",
					},
				},
				Usage: Usage{
					PromptTokens:     10,
					CompletionTokens: 20,
					TotalTokens:      30,
				},
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		// Create client with mock server URL
		client := NewClient(Config{APIKey: "test-key"})
		client.baseURL = server.URL
		client.SetHTTPClient(server.Client()) // Override SSRF-safer client for localhost testing

		// Test request
		resp, err := client.Chat(context.Background(), ChatRequest{
			SystemPrompt: "You are a test assistant",
			UserPrompt:   "Hello, world!",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "Test response content" {
			t.Errorf("expected response content, got %s", resp.Content)
		}
		if resp.Usage.TotalTokens != 30 {
			t.Errorf("expected 30 total tokens, got %d", resp.Usage.TotalTokens)
		}
	})

	t.Run("empty API key returns error", func(t *testing.T) {
		client := NewClient(Config{}) // No API key

		_, err := client.Chat(context.Background(), ChatRequest{
			UserPrompt: "Hello",
		})

		if err == nil {
			t.Fatal("expected error for missing API key, got nil")
		}
		if !strings.Contains(err.Error(), "API key not configured") {
			t.Errorf("expected API key error, got: %v", err)
		}
	})

	t.Run("request parameter overrides", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var reqBody ChatCompletionRequest
			json.NewDecoder(r.Body).Decode(&reqBody)

			// Verify overrides were applied
			if reqBody.Temperature != 0.9 {
				t.Errorf("expected temperature 0.9, got %f", reqBody.Temperature)
			}
			if reqBody.MaxTokens != 500 {
				t.Errorf("expected max tokens 500, got %d", reqBody.MaxTokens)
			}
			if reqBody.Model != "custom/model" {
				t.Errorf("expected custom model, got %s", reqBody.Model)
			}

			// Send mock response
			response := ChatCompletionResponse{
				Choices: []Choice{{Message: Message{Content: "test"}}},
				Usage:   Usage{},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		client := NewClient(Config{APIKey: "test-key"})
		client.baseURL = server.URL
		client.SetHTTPClient(server.Client()) // Override SSRF-safer client for localhost testing

		temperature := 0.9
		maxTokens := 500
		model := "custom/model"

		_, err := client.Chat(context.Background(), ChatRequest{
			UserPrompt:  "test",
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
			Model:       &model,
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestClient_RetryLogic tests the retry functionality
func TestClient_RetryLogic(t *testing.T) {
	t.Run("doesn't retry HTTP errors (correct behavior)", func(t *testing.T) {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}))
		defer server.Close()

		client := NewClient(Config{APIKey: "test-key"})
		client.baseURL = server.URL
		client.SetHTTPClient(server.Client()) // Override SSRF-safer client for localhost testing

		_, err := client.Chat(context.Background(), ChatRequest{
			UserPrompt: "test",
		})

		if err == nil {
			t.Fatal("expected error for HTTP 500")
		}
		if requestCount != 1 {
			t.Errorf("expected 1 request (no retries for HTTP errors), got %d", requestCount)
		}
	})

	t.Run("tests retry error detection logic", func(t *testing.T) {
		client := NewClient(Config{APIKey: "test-key"})

		// Test cases for retryable errors
		retryableErrors := []error{
			&net.DNSError{Err: "no such host", IsTimeout: true},
		}

		for _, err := range retryableErrors {
			if !client.isRetryableError(err) {
				t.Errorf("expected %v to be retryable", err)
			}
		}

		// Test non-retryable errors
		nonRetryableErrors := []error{
			&net.DNSError{Err: "no such host", IsTimeout: false},
		}

		for _, err := range nonRetryableErrors {
			if client.isRetryableError(err) {
				t.Errorf("expected %v to NOT be retryable", err)
			}
		}
	})

	t.Run("tests error string matching", func(t *testing.T) {
		client := NewClient(Config{APIKey: "test-key"})

		// Test network error string detection
		testCases := []struct {
			errorStr  string
			retryable bool
		}{
			{"connection reset by peer", true},
			{"connection refused", true},
			{"timeout", true},
			{"i/o timeout", true},
			{"network is unreachable", true},
			{"temporary failure", true},
			{"invalid json", false},
			{"unauthorized", false},
		}

		for _, tc := range testCases {
			err := &testError{msg: tc.errorStr}
			result := client.isRetryableError(err)
			if result != tc.retryable {
				t.Errorf("error %q: expected retryable=%v, got %v", tc.errorStr, tc.retryable, result)
			}
		}
	})
}

// testError is a simple error type for testing error string matching
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// TestClient_ErrorHandling tests various error scenarios
func TestClient_ErrorHandling(t *testing.T) {
	t.Run("handles malformed JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		client := NewClient(Config{APIKey: "test-key"})
		client.baseURL = server.URL
		client.SetHTTPClient(server.Client()) // Override SSRF-safer client for localhost testing

		_, err := client.Chat(context.Background(), ChatRequest{
			UserPrompt: "test",
		})

		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("handles empty choices", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := ChatCompletionResponse{
				Choices: []Choice{}, // Empty choices
				Usage:   Usage{},
			}
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		client := NewClient(Config{APIKey: "test-key"})
		client.baseURL = server.URL
		client.SetHTTPClient(server.Client()) // Override SSRF-safer client for localhost testing

		_, err := client.Chat(context.Background(), ChatRequest{
			UserPrompt: "test",
		})

		if err == nil {
			t.Fatal("expected error for empty choices")
		}
		if !strings.Contains(err.Error(), "no response choices") {
			t.Errorf("expected 'no response choices' error, got: %v", err)
		}
	})
}

// Benchmark tests to ensure performance is acceptable
func BenchmarkClient_Chat(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ChatCompletionResponse{
			Choices: []Choice{{Message: Message{Content: "test response"}}},
			Usage:   Usage{TotalTokens: 10},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key"})
	client.baseURL = server.URL
	client.SetHTTPClient(server.Client()) // Override SSRF-safe client for localhost testing

	ctx := context.Background()
	req := ChatRequest{
		UserPrompt: "Hello",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Chat(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}
