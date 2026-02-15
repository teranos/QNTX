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

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ai/tracker"
	"github.com/teranos/QNTX/errors"
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
	httpClient   *httpclient.SaferClient
	config       Config
	usageTracker *tracker.UsageTracker
	logger       *zap.SugaredLogger
}

// Config holds AI client configuration
type Config struct {
	APIKey        string
	Model         string
	Temperature   *float64 // nil = use default (0.2)
	MaxTokens     *int     // nil = use default (1000)
	Debug         bool
	Logger        *zap.SugaredLogger // Structured logger (nil = nop logger)
	DB            *sql.DB            // Database for automatic cost/usage tracking (strongly recommended)
	Verbosity     int                // Verbosity level for usage tracking output
	OperationType string             // Operation type for tracking context (e.g., "code-analysis")
	EntityType    string             // Entity type for tracking context (e.g., "file")
	EntityID      string             // Entity ID for tracking context (e.g., file path)
}

// NewClient creates a new OpenRouter.ai client with QNTX-specific defaults
func NewClient(config Config) *Client {
	if config.Model == "" {
		config.Model = DefaultModel
	}
	if config.Temperature == nil {
		defaultTemp := 0.2
		config.Temperature = &defaultTemp
	}
	if config.MaxTokens == nil {
		defaultTokens := 1000
		config.MaxTokens = &defaultTokens
	}

	// Initialize usage tracker if database is provided
	var usageTracker *tracker.UsageTracker
	if config.DB != nil {
		usageTracker = tracker.NewUsageTracker(config.DB, config.Verbosity)
	}

	// Initialize logger (nop if not provided)
	logger := config.Logger
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}

	// Create SSRF-safer HTTP client with redirect protection
	// Blocks private IPs, localhost, AWS metadata endpoint, dangerous schemes
	blockPrivateIP := true
	saferClient := httpclient.NewSaferClientWithOptions(120*time.Second, httpclient.SaferClientOptions{
		BlockPrivateIP: &blockPrivateIP,
	})

	return &Client{
		apiKey:       config.APIKey,
		baseURL:      "https://openrouter.ai/api/v1",
		httpClient:   saferClient,
		config:       config,
		usageTracker: usageTracker,
		logger:       logger,
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
	Attachments  []ContentPart // Multimodal attachments (base64 documents/images) — not serialized to JSON
}

// ChatResponse represents the AI response
type ChatResponse struct {
	Content string
	Usage   Usage
}

// ContentPart represents a single part in a multimodal message content array.
// Used for text, images, and file attachments in OpenRouter's content array format.
//
// Images use type "image_url" with a data URI.
// Files (PDFs) use type "file" with filename + data URI.
type ContentPart struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *ContentPartImage `json:"image_url,omitempty"`
	File     *ContentPartFile  `json:"file,omitempty"`
}

// ContentPartImage holds a data URI for an image attachment.
// URL is a data URI: "data:{mime};base64,{data}"
type ContentPartImage struct {
	URL string `json:"url"`
}

// ContentPartFile holds a file attachment (e.g. PDF).
// FileData is a data URI: "data:{mime};base64,{data}"
type ContentPartFile struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

// Message represents a message in a chat completion.
// Content is json.RawMessage so it can serialize as either a plain string
// (for text-only) or a []ContentPart array (for multimodal).
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// NewTextMessage creates a Message with plain text content (serialized as a JSON string).
func NewTextMessage(role, text string) Message {
	raw, _ := json.Marshal(text)
	return Message{Role: role, Content: raw}
}

// NewMultimodalMessage creates a Message with a content parts array (text + attachments).
func NewMultimodalMessage(role, text string, attachments []ContentPart) Message {
	parts := make([]ContentPart, 0, 1+len(attachments))
	parts = append(parts, ContentPart{Type: "text", Text: text})
	parts = append(parts, attachments...)
	raw, _ := json.Marshal(parts)
	return Message{Role: role, Content: raw}
}

// TextContent extracts the plain text from Content.
// LLM responses are always plain strings; this unmarshals back from json.RawMessage.
func (m Message) TextContent() string {
	var s string
	if err := json.Unmarshal(m.Content, &s); err != nil {
		// Fallback: return raw bytes as string (shouldn't happen for LLM responses)
		return string(m.Content)
	}
	return s
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
		return nil, errors.Wrap(err, "failed to marshal request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
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
		return nil, errors.Wrap(err, "failed to send request")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal response")
	}

	return &chatResp, nil
}

// Chat sends a chat completion request to OpenAI with retry logic and QNTX-specific functionality
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Handle API key validation
	if c.config.APIKey == "" {
		return nil, errors.New("OpenRouter API key not configured")
	}

	// Prepare request parameters (dereference config defaults, allow per-request overrides)
	temperature := *c.config.Temperature
	if req.Temperature != nil {
		temperature = *req.Temperature
	}

	maxTokens := *c.config.MaxTokens
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	model := c.config.Model
	if req.Model != nil {
		model = *req.Model
	}

	c.logger.Debugw("AI Chat Request",
		"model", model,
		"temperature", temperature,
		"max_tokens", maxTokens,
		"system_prompt", req.SystemPrompt,
		"user_prompt", req.UserPrompt,
	)

	// Prepare OpenRouter request
	var userMsg Message
	if len(req.Attachments) > 0 {
		userMsg = NewMultimodalMessage("user", req.UserPrompt, req.Attachments)
	} else {
		userMsg = NewTextMessage("user", req.UserPrompt)
	}
	messages := []Message{userMsg}

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		messages = append([]Message{NewTextMessage("system", req.SystemPrompt)}, messages...)
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
			c.logger.Debugw("Retrying OpenRouter request",
				"attempt", attempt, "max_retries", maxRetries-1, "delay", delay)
			time.Sleep(delay)
		}

		resp, err = c.CreateChatCompletion(ctx, openrouterReq)

		// Success
		if err == nil {
			if attempt > 0 {
				c.logger.Infow("Request succeeded after retries", "attempts", attempt+1, "model", model)
			}
			break
		}

		// Log error details on first attempt or in debug
		c.logger.Warnw("OpenRouter API error",
			"attempt", attempt+1, "max_retries", maxRetries,
			"error", err, "model", model,
			"url", c.baseURL+"/chat/completions")

		// Check if retryable
		if c.isRetryableError(err) {
			c.logger.Debugw("Retryable error detected, will retry", "error", err)
			continue
		}

		// Non-retryable error - provide detailed info
		c.trackFailedRequest(requestTime, model, temperature, maxTokens, err)
		return nil, errors.Wrap(err, "OpenRouter API error")
	}

	if err != nil {
		c.trackFailedRequest(requestTime, model, temperature, maxTokens, err)
		return nil, errors.Wrapf(err, "OpenRouter API error after %d retries", maxRetries)
	}

	// Validate response before accessing
	if len(resp.Choices) == 0 {
		return nil, errors.New("no response choices from OpenRouter")
	}

	responseText := resp.Choices[0].Message.TextContent()

	c.logger.Debugw("OpenRouter response",
		"content_length", len(responseText),
		"prompt_tokens", resp.Usage.PromptTokens,
		"completion_tokens", resp.Usage.CompletionTokens,
		"total_tokens", resp.Usage.TotalTokens,
	)

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
			c.logger.Warnw("Failed to track usage", "error", err, "model", model, "tokens", tokensUsed)
		}
	}

	return &ChatResponse{
		Content: strings.TrimSpace(responseText),
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
		c.logger.Warnw("Failed to track failed request", "error", trackErr, "model", model, "original_error", err.Error())
	}
}

// IsConfigured returns true if the client has a valid API key
func (c *Client) IsConfigured() bool {
	return c.config.APIKey != ""
}

// SetHTTPClient allows overriding the HTTP client for testing
// ⚠️ WARNING: Only use this in tests. Production code should use the default SSRF-safer client.
func (c *Client) SetHTTPClient(client *http.Client) {
	c.httpClient = httpclient.WrapClient(client)
}
