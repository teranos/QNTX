package openrouter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/teranos/QNTX/ai/tracker"
	"github.com/teranos/QNTX/internal/httpclient"
)

const (
	// DefaultModel is the fallback model when none is specified
	// Should match the default in am/defaults.go for consistency
	DefaultModel = "openai/gpt-4o-mini"
)

// Client represents an OpenRouter.ai API client with QNTX-specific functionality
type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	config       Config
	usageTracker *tracker.UsageTracker
}

// Config holds AI client configuration
type Config struct {
	APIKey        string
	Model         string
	Temperature   float64
	MaxTokens     int
	Debug         bool
	DB            *sql.DB // Database for automatic cost/usage tracking (strongly recommended)
	Verbosity     int     // Verbosity level for usage tracking output
	OperationType string  // Operation type for tracking context (e.g., "code-analysis")
	EntityType    string  // Entity type for tracking context (e.g., "file")
	EntityID      string  // Entity ID for tracking context (e.g., file path)
}

// NewClient creates a new OpenRouter.ai client with QNTX-specific defaults
func NewClient(config Config) *Client {
	if config.Model == "" {
		config.Model = DefaultModel
	}
	if config.Temperature == 0 {
		config.Temperature = 0.2 // Default to deterministic
	}
	if config.MaxTokens == 0 {
		config.MaxTokens = 1000 // Default token limit
	}

	// Initialize usage tracker if database is provided
	var usageTracker *tracker.UsageTracker
	if config.DB != nil {
		usageTracker = tracker.NewUsageTracker(config.DB, config.Verbosity)
	}

	// Create SSRF-safer HTTP client with redirect protection
	// Blocks private IPs, localhost, AWS metadata endpoint, dangerous schemes
	// Note: Tests use httptest.NewServer which binds to 127.0.0.1, so tests
	// will need to either mock the HTTP client or use a public test endpoint
	blockPrivateIP := true
	saferClient := httpclient.NewSaferClientWithOptions(120*time.Second, httpclient.SaferClientOptions{
		BlockPrivateIP: &blockPrivateIP,
	})

	return &Client{
		apiKey:       config.APIKey,
		baseURL:      "https://openrouter.ai/api/v1",
		httpClient:   saferClient.Client, // Use underlying http.Client
		config:       config,
		usageTracker: usageTracker,
	}
}

// NewClientWithAPIKey creates a new OpenRouter.ai client with just an API key (for backward compatibility)
func NewClientWithAPIKey(apiKey string) *Client {
	return NewClient(Config{APIKey: apiKey})
}

// ChatCompletionRequest represents a request to the chat completions endpoint
type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// ChatRequest represents a high-level request to the AI
type ChatRequest struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  *float64 // Override default temperature
	MaxTokens    *int     // Override default max tokens
	Model        *string  // Override default model
}

// ChatResponse represents the AI response
type ChatResponse struct {
	Content string
	Usage   Usage
}

// Message represents a message in a chat completion
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionResponse represents the response from chat completions
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a completion choice
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// CreateChatCompletion sends a chat completion request to OpenRouter
func (c *Client) CreateChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Set X-Title header for OpenRouter dashboard tracking
	if c.config.OperationType != "" {
		httpReq.Header.Set("X-Title", fmt.Sprintf("qntx/%s", c.config.OperationType))
	} else {
		httpReq.Header.Set("X-Title", "qntx")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &chatResp, nil
}

// Chat sends a chat completion request to OpenAI with retry logic and QNTX-specific functionality
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Handle API key validation
	if c.config.APIKey == "" {
		return nil, fmt.Errorf("OpenRouter API key not configured")
	}

	// Prepare request parameters
	temperature := c.config.Temperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}

	maxTokens := c.config.MaxTokens
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	model := c.config.Model
	if req.Model != nil {
		model = *req.Model
	}

	// Debug output
	if c.config.Debug {
		fmt.Printf("\n🤖 AI Chat Request\n")
		fmt.Printf("Model: %s\n", model)
		fmt.Printf("Temperature: %.2f\n", temperature)
		fmt.Printf("Max Tokens: %d\n", maxTokens)
		fmt.Printf("System: %s\n", req.SystemPrompt)
		fmt.Printf("User: %s\n", req.UserPrompt)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}

	// Prepare OpenRouter request
	messages := []Message{
		{
			Role:    "user",
			Content: req.UserPrompt,
		},
	}

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		messages = append([]Message{
			{
				Role:    "system",
				Content: req.SystemPrompt,
			},
		}, messages...)
	}

	openrouterReq := ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	}

	// Track usage if tracker is available
	requestTime := time.Now()

	// Retry logic inspired by Lingo
	maxRetries := 3
	var resp *ChatCompletionResponse
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * time.Second
			if c.config.Debug {
				fmt.Printf("🔄 Retry attempt %d/%d, waiting %v...\n", attempt, maxRetries-1, delay)
			}
			time.Sleep(delay)
		}

		resp, err = c.CreateChatCompletion(ctx, openrouterReq)

		// Success
		if err == nil {
			if attempt > 0 && c.config.Debug {
				fmt.Printf("✅ Request succeeded after %d attempts\n", attempt+1)
			}
			break
		}

		// Always show detailed error info for debugging
		if c.config.Debug || attempt == 0 {
			fmt.Printf("❌ OpenRouter API Error (attempt %d/%d):\n", attempt+1, maxRetries)
			fmt.Printf("🔍 Error: %v\n", err)
			fmt.Printf("🔗 Model: %s\n", model)
			fmt.Printf("📡 URL: %s/chat/completions\n", "https://openrouter.ai/api/v1")
		}

		// Check if retryable
		if c.isRetryableError(err) {
			if c.config.Debug {
				fmt.Printf("🌐 Retryable error, will retry...\n")
			}
			continue
		}

		// Non-retryable error - provide detailed info
		c.trackFailedRequest(requestTime, model, temperature, maxTokens, err)
		return nil, fmt.Errorf("OpenRouter API error: %w", err)
	}

	if err != nil {
		c.trackFailedRequest(requestTime, model, temperature, maxTokens, err)
		return nil, fmt.Errorf("OpenRouter API error after %d retries: %w", maxRetries, err)
	}

	// Validate response before accessing
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response choices from OpenRouter")
	}

	// Debug response (debug mode shows full details)
	if c.config.Debug {
		fmt.Printf("🔍 OpenRouter Response:\n")
		fmt.Printf("Content: %s\n", resp.Choices[0].Message.Content)
		fmt.Printf("Usage: prompt=%d, completion=%d, total=%d\n",
			resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}

	// Track successful usage
	if c.usageTracker != nil {
		responseTime := time.Now()
		tokensUsed := resp.Usage.TotalTokens
		modelConfig := tracker.NewModelConfig(&temperature, &maxTokens)

		// Calculate cost based on model pricing
		cost := CalculateCost(model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

		usage := &tracker.ModelUsage{
			OperationType:     c.config.OperationType,
			EntityType:        c.config.EntityType,
			EntityID:          c.config.EntityID,
			ModelName:         model,
			ModelProvider:     "openrouter",
			ModelConfig:       modelConfig,
			RequestTimestamp:  requestTime,
			ResponseTimestamp: &responseTime,
			TokensUsed:        &tokensUsed,
			Cost:              &cost,
			Success:           true,
			ErrorMessage:      nil,
		}

		if err := c.usageTracker.TrackUsage(usage); err != nil {
			// Always log tracking errors (budget system relies on this data)
			fmt.Printf("⚠️  Failed to track usage: %v\n", err)
		}
	}

	return &ChatResponse{
		Content: strings.TrimSpace(resp.Choices[0].Message.Content),
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}, nil
}

// isRetryableError checks if an error is worth retrying (network-related)
func (c *Client) isRetryableError(err error) bool {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}

	if syscallErr, ok := err.(*net.OpError); ok {
		if errno, ok := syscallErr.Err.(syscall.Errno); ok {
			switch errno {
			case syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ETIMEDOUT:
				return true
			}
		}
	}

	// Check for common network error strings
	errStr := strings.ToLower(err.Error())
	networkErrors := []string{
		"connection reset by peer",
		"connection refused",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"i/o timeout",
	}

	for _, netErr := range networkErrors {
		if strings.Contains(errStr, netErr) {
			return true
		}
	}

	return false
}

// trackFailedRequest tracks a failed API request
func (c *Client) trackFailedRequest(requestTime time.Time, model string, temperature float64, maxTokens int, err error) {
	if c.usageTracker == nil {
		return
	}

	responseTime := time.Now()
	errMsg := err.Error()
	modelConfig := tracker.NewModelConfig(&temperature, &maxTokens)

	usage := &tracker.ModelUsage{
		OperationType:     c.config.OperationType,
		EntityType:        c.config.EntityType,
		EntityID:          c.config.EntityID,
		ModelName:         model,
		ModelProvider:     "openrouter",
		ModelConfig:       modelConfig,
		RequestTimestamp:  requestTime,
		ResponseTimestamp: &responseTime,
		TokensUsed:        nil,
		Cost:              nil,
		Success:           false,
		ErrorMessage:      &errMsg,
	}

	if trackErr := c.usageTracker.TrackUsage(usage); trackErr != nil {
		// Always log tracking errors (budget system relies on this data)
		fmt.Printf("⚠️  Failed to track failed request: %v\n", trackErr)
	}
}

// IsConfigured returns true if the client has a valid API key
func (c *Client) IsConfigured() bool {
	return c.config.APIKey != ""
}

// SetHTTPClient allows overriding the HTTP client for testing
// ⚠️ WARNING: Only use this in tests. Production code should use the default SSRF-safer client.
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}
