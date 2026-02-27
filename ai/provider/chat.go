package provider

import "encoding/json"

// ChatRequest represents a high-level request to an LLM provider.
// This is the unified request type used by all providers (OpenRouter, local, etc.)
type ChatRequest struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  *float64      // Override default temperature
	MaxTokens    *int          // Override default max tokens
	Model        *string       // Override default model
	Attachments  []ContentPart // Multimodal attachments (base64 documents/images)
}

// ChatResponse represents the LLM response.
type ChatResponse struct {
	Content string
	Usage   Usage
}

// Usage represents token usage information from an LLM call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ContentPart represents a single part in a multimodal message content array.
type ContentPart struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	ImageURL *ContentPartImage `json:"image_url,omitempty"`
	File     *ContentPartFile  `json:"file,omitempty"`
}

// ContentPartImage holds a data URI for an image attachment.
type ContentPartImage struct {
	URL string `json:"url"`
}

// ContentPartFile holds a file attachment (e.g. PDF).
type ContentPartFile struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

// Message represents a message in a chat completion.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// NewTextMessage creates a Message with plain text content.
func NewTextMessage(role, text string) Message {
	raw, _ := json.Marshal(text)
	return Message{Role: role, Content: raw}
}

// NewMultimodalMessage creates a Message with a content parts array.
func NewMultimodalMessage(role, text string, attachments []ContentPart) Message {
	parts := make([]ContentPart, 0, 1+len(attachments))
	parts = append(parts, ContentPart{Type: "text", Text: text})
	parts = append(parts, attachments...)
	raw, _ := json.Marshal(parts)
	return Message{Role: role, Content: raw}
}

// TextContent extracts the plain text from Content.
func (m Message) TextContent() string {
	var s string
	if err := json.Unmarshal(m.Content, &s); err != nil {
		return string(m.Content)
	}
	return s
}
