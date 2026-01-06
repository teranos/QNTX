package anthropic

import (
	"bufio"
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

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/ai/tracker"
	"github.com/teranos/QNTX/internal/httpclient"
)

const (
	// DefaultModel is the default Claude model
	DefaultModel = "claude-sonnet-4-20250514"

	// BaseURL is the Anthropic API endpoint
	BaseURL = "https://api.anthropic.com/v1"

	// APIVersion is the required Anthropic API version header
	APIVersion = "2023-06-01"
)

// Client represents an Anthropic API client
type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	config       Config
	usageTracker *tracker.UsageTracker
}

// Config holds Anthropic client configuration
type Config struct {
	APIKey        string
	Model         string
	Temperature   float64
	MaxTokens     int
	Debug         bool
	DB            *sql.DB // Database for automatic cost/usage tracking
	Verbosity     int     // Verbosity level for usage tracking output
	OperationType string  // Operation type for tracking context
	EntityType    string  // Entity type for tracking context
	EntityID      string  // Entity ID for tracking context
}

// NewClient creates a new Anthropic API client
func NewClient(config Config) *Client {
	if config.Model == "" {
		config.Model = DefaultModel
	}
	if config.Temperature == 0 {
		config.Temperature = 0.2 // Deterministic default
	}
	if config.MaxTokens == 0 {
		config.MaxTokens = 4096 // Higher default for Claude
	}

	// Initialize usage tracker if database is provided
	var usageTracker *tracker.UsageTracker
	if config.DB != nil {
		usageTracker = tracker.NewUsageTracker(config.DB, config.Verbosity)
	}

	// Create SSRF-safer HTTP client
	blockPrivateIP := true
	saferClient := httpclient.NewSaferClientWithOptions(120*time.Second, httpclient.SaferClientOptions{
		BlockPrivateIP: &blockPrivateIP,
	})

	return &Client{
		apiKey:       config.APIKey,
		baseURL:      BaseURL,
		httpClient:   saferClient.Client,
		config:       config,
		usageTracker: usageTracker,
	}
}

// MessagesRequest represents a request to the Anthropic Messages API
type MessagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	Messages    []Message `json:"messages"`
	System      string    `json:"system,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Message represents a message in the conversation
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// MessagesResponse represents the response from the Messages API
type MessagesResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence,omitempty"`
	Usage        Usage          `json:"usage"`
}

// ContentBlock represents a content block in the response
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// StreamEvent represents a streaming event from the API
type StreamEvent struct {
	Type         string          `json:"type"`
	Message      *MessagesResponse `json:"message,omitempty"`
	Index        int             `json:"index,omitempty"`
	ContentBlock *ContentBlock   `json:"content_block,omitempty"`
	Delta        *StreamDelta    `json:"delta,omitempty"`
	Usage        *Usage          `json:"usage,omitempty"`
}

// StreamDelta represents incremental content in streaming
type StreamDelta struct {
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}

// Chat implements the AIClient interface for Anthropic
// This allows seamless switching between providers
func (c *Client) Chat(ctx context.Context, req openrouter.ChatRequest) (*openrouter.ChatResponse, error) {
	if c.config.APIKey == "" {
		return nil, fmt.Errorf("Anthropic API key not configured")
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

	if c.config.Debug {
		fmt.Printf("\n🤖 Anthropic Chat Request\n")
		fmt.Printf("Model: %s\n", model)
		fmt.Printf("Temperature: %.2f\n", temperature)
		fmt.Printf("Max Tokens: %d\n", maxTokens)
		fmt.Printf("System: %s\n", req.SystemPrompt)
		fmt.Printf("User: %s\n", req.UserPrompt)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}

	// Build Anthropic Messages request
	anthropicReq := MessagesRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		System:      req.SystemPrompt,
		Messages: []Message{
			{Role: "user", Content: req.UserPrompt},
		},
	}

	requestTime := time.Now()

	// Retry logic
	maxRetries := 3
	var resp *MessagesResponse
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * time.Second
			if c.config.Debug {
				fmt.Printf("🔄 Retry attempt %d/%d, waiting %v...\n", attempt, maxRetries-1, delay)
			}
			time.Sleep(delay)
		}

		resp, err = c.createMessages(ctx, anthropicReq)
		if err == nil {
			break
		}

		if c.config.Debug {
			fmt.Printf("❌ Anthropic API Error (attempt %d/%d): %v\n", attempt+1, maxRetries, err)
		}

		if !c.isRetryableError(err) {
			c.trackFailedRequest(requestTime, model, temperature, maxTokens, err)
			return nil, fmt.Errorf("Anthropic API error: %w", err)
		}
	}

	if err != nil {
		c.trackFailedRequest(requestTime, model, temperature, maxTokens, err)
		return nil, fmt.Errorf("Anthropic API error after %d retries: %w", maxRetries, err)
	}

	// Extract text content
	var content strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}

	if c.config.Debug {
		fmt.Printf("🔍 Anthropic Response:\n")
		fmt.Printf("Content: %s\n", content.String())
		fmt.Printf("Usage: input=%d, output=%d\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	}

	// Track usage
	if c.usageTracker != nil {
		responseTime := time.Now()
		totalTokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
		modelConfig := tracker.NewModelConfig(&temperature, &maxTokens)
		cost := CalculateCost(model, resp.Usage.InputTokens, resp.Usage.OutputTokens)

		usage := &tracker.ModelUsage{
			OperationType:     c.config.OperationType,
			EntityType:        c.config.EntityType,
			EntityID:          c.config.EntityID,
			ModelName:         model,
			ModelProvider:     "anthropic",
			ModelConfig:       modelConfig,
			RequestTimestamp:  requestTime,
			ResponseTimestamp: &responseTime,
			TokensUsed:        &totalTokens,
			Cost:              &cost,
			Success:           true,
			ErrorMessage:      nil,
		}

		if err := c.usageTracker.TrackUsage(usage); err != nil {
			fmt.Printf("⚠️  Failed to track usage: %v\n", err)
		}
	}

	return &openrouter.ChatResponse{
		Content: strings.TrimSpace(content.String()),
		Usage: openrouter.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}, nil
}

// createMessages sends a request to the Anthropic Messages API
func (c *Client) createMessages(ctx context.Context, req MessagesRequest) (*MessagesResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", APIVersion)

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

	var messagesResp MessagesResponse
	if err := json.Unmarshal(respBody, &messagesResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &messagesResp, nil
}

// StreamChunk represents a chunk of streamed response
type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}

// ChatStreaming implements streaming chat for Anthropic
func (c *Client) ChatStreaming(ctx context.Context, req openrouter.ChatRequest, streamChan chan<- StreamChunk) error {
	if c.config.APIKey == "" {
		return fmt.Errorf("Anthropic API key not configured")
	}

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

	anthropicReq := MessagesRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		System:      req.SystemPrompt,
		Stream:      true,
		Messages: []Message{
			{Role: "user", Content: req.UserPrompt},
		},
	}

	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/messages", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", APIVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Check for stream end
			if data == "[DONE]" {
				streamChan <- StreamChunk{Done: true}
				return nil
			}

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue // Skip malformed events
			}

			// Handle content delta events
			if event.Type == "content_block_delta" && event.Delta != nil {
				if event.Delta.Text != "" {
					streamChan <- StreamChunk{Content: event.Delta.Text}
				}
			}

			// Handle message stop
			if event.Type == "message_stop" {
				streamChan <- StreamChunk{Done: true}
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %w", err)
	}

	streamChan <- StreamChunk{Done: true}
	return nil
}

// isRetryableError checks if an error is worth retrying
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

	errStr := strings.ToLower(err.Error())
	networkErrors := []string{
		"connection reset by peer",
		"connection refused",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"i/o timeout",
		"overloaded", // Anthropic-specific
		"529",        // Anthropic overloaded status
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
		ModelProvider:     "anthropic",
		ModelConfig:       modelConfig,
		RequestTimestamp:  requestTime,
		ResponseTimestamp: &responseTime,
		TokensUsed:        nil,
		Cost:              nil,
		Success:           false,
		ErrorMessage:      &errMsg,
	}

	if trackErr := c.usageTracker.TrackUsage(usage); trackErr != nil {
		fmt.Printf("⚠️  Failed to track failed request: %v\n", trackErr)
	}
}

// IsConfigured returns true if the client has a valid API key
func (c *Client) IsConfigured() bool {
	return c.config.APIKey != ""
}

// SetHTTPClient allows overriding the HTTP client for testing
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = client
}
