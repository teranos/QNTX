package prompt

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantName    string
		wantModel   string
		wantTemp    *float64
		wantTokens  *int
		wantPrivacy string
		wantBody    string
		wantErr     bool
	}{
		{
			name: "full frontmatter",
			input: `---
name: "recipe-generator"
description: "Generate recipes from ingredients"
version: "2.0"
model: "anthropic/claude-sonnet-4"
temperature: 0.7
max_tokens: 2000
privacy: "private"
---
Generate a recipe using these ingredients:
{{attributes.ingredients}}`,
			wantName:    "recipe-generator",
			wantModel:   "anthropic/claude-sonnet-4",
			wantTemp:    floatPtr(0.7),
			wantTokens:  intPtr(2000),
			wantPrivacy: "private",
			wantBody:    "Generate a recipe using these ingredients:\n{{attributes.ingredients}}",
			wantErr:     false,
		},
		{
			name: "minimal frontmatter",
			input: `---
name: "simple-prompt"
---
Just a prompt body`,
			wantName: "simple-prompt",
			wantBody: "Just a prompt body",
			wantErr:  false,
		},
		{
			name:     "no frontmatter",
			input:    "Just plain text without frontmatter",
			wantBody: "Just plain text without frontmatter",
			wantErr:  false,
		},
		{
			name: "empty frontmatter",
			input: `---
---
Body after empty frontmatter`,
			wantBody: "Body after empty frontmatter",
			wantErr:  false,
		},
		{
			name: "invalid temperature",
			input: `---
name: "bad-temp"
temperature: 3.0
---
Body`,
			wantErr: true,
		},
		{
			name: "invalid privacy",
			input: `---
name: "bad-privacy"
privacy: "secret"
---
Body`,
			wantErr: true,
		},
		{
			name: "negative max_tokens",
			input: `---
name: "bad-tokens"
max_tokens: -100
---
Body`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseFrontmatter(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFrontmatter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if doc.Metadata.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", doc.Metadata.Name, tt.wantName)
			}
			if doc.Metadata.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", doc.Metadata.Model, tt.wantModel)
			}
			if !floatPtrEqual(doc.Metadata.Temperature, tt.wantTemp) {
				t.Errorf("Temperature = %v, want %v", doc.Metadata.Temperature, tt.wantTemp)
			}
			if !intPtrEqual(doc.Metadata.MaxTokens, tt.wantTokens) {
				t.Errorf("MaxTokens = %v, want %v", doc.Metadata.MaxTokens, tt.wantTokens)
			}
			if doc.Metadata.Privacy != tt.wantPrivacy {
				t.Errorf("Privacy = %q, want %q", doc.Metadata.Privacy, tt.wantPrivacy)
			}
			if doc.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", doc.Body, tt.wantBody)
			}
		})
	}
}

func TestPromptDocument_GetMethods(t *testing.T) {
	doc := &PromptDocument{
		Metadata: PromptMetadata{
			Model:       "anthropic/claude-sonnet-4",
			Temperature: floatPtr(0.8),
			MaxTokens:   intPtr(1500),
			Privacy:     "team",
		},
	}

	if got := doc.GetModel("default-model"); got != "anthropic/claude-sonnet-4" {
		t.Errorf("GetModel() = %q, want %q", got, "anthropic/claude-sonnet-4")
	}

	if got := doc.GetTemperature(0.5); got != 0.8 {
		t.Errorf("GetTemperature() = %f, want %f", got, 0.8)
	}

	if got := doc.GetMaxTokens(1000); got != 1500 {
		t.Errorf("GetMaxTokens() = %d, want %d", got, 1500)
	}

	if got := doc.GetPrivacy(); got != "team" {
		t.Errorf("GetPrivacy() = %q, want %q", got, "team")
	}
}

func TestPromptDocument_GetMethods_UseFallback(t *testing.T) {
	doc := &PromptDocument{
		Metadata: PromptMetadata{},
	}

	if got := doc.GetModel("fallback-model"); got != "fallback-model" {
		t.Errorf("GetModel() = %q, want %q", got, "fallback-model")
	}

	if got := doc.GetTemperature(0.5); got != 0.5 {
		t.Errorf("GetTemperature() = %f, want %f", got, 0.5)
	}

	if got := doc.GetMaxTokens(1000); got != 1000 {
		t.Errorf("GetMaxTokens() = %d, want %d", got, 1000)
	}

	// Privacy defaults to "private"
	if got := doc.GetPrivacy(); got != "private" {
		t.Errorf("GetPrivacy() = %q, want %q", got, "private")
	}
}

// Helper functions
func floatPtr(f float64) *float64 {
	return &f
}

func intPtr(i int) *int {
	return &i
}

func floatPtrEqual(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func intPtrEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
