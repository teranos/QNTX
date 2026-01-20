package prompt

import (
	"strings"

	"github.com/teranos/QNTX/errors"
	"gopkg.in/yaml.v3"
)

// PromptDocument represents a prompt with frontmatter metadata and template body
type PromptDocument struct {
	Metadata PromptMetadata
	Body     string
}

// PromptMetadata holds configuration from YAML frontmatter
type PromptMetadata struct {
	// Name is the prompt identifier
	Name string `yaml:"name"`

	// Description explains what the prompt does
	Description string `yaml:"description"`

	// Version for tracking prompt evolution
	Version string `yaml:"version"`

	// Model specifies which LLM to use (overrides config default)
	// Format: "provider/model" (e.g., "anthropic/claude-sonnet-4")
	Model string `yaml:"model,omitempty"`

	// Temperature controls randomness (0.0-2.0, provider-dependent)
	Temperature *float64 `yaml:"temperature,omitempty"`

	// MaxTokens limits response length
	MaxTokens *int `yaml:"max_tokens,omitempty"`

	// Type distinguishes system vs user prompts
	Type string `yaml:"type,omitempty"`

	// Variables lists expected template placeholders
	Variables []string `yaml:"variables,omitempty"`
}

// ParseFrontmatter extracts YAML frontmatter and body from a prompt document
// Expected format:
//
//	---
//	name: "prompt-name"
//	model: "anthropic/claude-sonnet-4"
//	temperature: 0.7
//	---
//	Prompt body with {{placeholders}}
func ParseFrontmatter(content string) (*PromptDocument, error) {
	// Split on --- delimiters
	parts := strings.SplitN(content, "---", 3)

	// No frontmatter - entire content is body
	if len(parts) < 3 {
		return &PromptDocument{
			Metadata: PromptMetadata{},
			Body:     content,
		}, nil
	}

	// First part should be empty (before first ---)
	// Second part is YAML frontmatter
	// Third part is body
	frontmatterYAML := strings.TrimSpace(parts[1])
	body := strings.TrimSpace(parts[2])

	// Parse YAML frontmatter
	var metadata PromptMetadata
	if frontmatterYAML != "" {
		if err := yaml.Unmarshal([]byte(frontmatterYAML), &metadata); err != nil {
			return nil, errors.Wrap(err, "failed to parse frontmatter YAML")
		}
	}

	// Validate required fields
	if err := validateMetadata(&metadata); err != nil {
		return nil, errors.Wrap(err, "invalid frontmatter")
	}

	return &PromptDocument{
		Metadata: metadata,
		Body:     body,
	}, nil
}

// validateMetadata checks that required fields are present and valid
func validateMetadata(m *PromptMetadata) error {
	// Name is recommended but not strictly required
	// (prompts can be anonymous for testing)

	// Validate temperature range if provided
	if m.Temperature != nil {
		if *m.Temperature < 0.0 || *m.Temperature > 2.0 {
			return errors.Newf("temperature must be between 0.0 and 2.0, got %f", *m.Temperature)
		}
	}

	// Validate max_tokens if provided
	if m.MaxTokens != nil {
		if *m.MaxTokens < 1 {
			return errors.Newf("max_tokens must be positive, got %d", *m.MaxTokens)
		}
	}

	return nil
}

// GetModel returns the model specified in metadata, or fallback if not set
func (p *PromptDocument) GetModel(fallback string) string {
	if p.Metadata.Model != "" {
		return p.Metadata.Model
	}
	return fallback
}

// GetTemperature returns the temperature specified in metadata, or fallback if not set
func (p *PromptDocument) GetTemperature(fallback float64) float64 {
	if p.Metadata.Temperature != nil {
		return *p.Metadata.Temperature
	}
	return fallback
}

// GetMaxTokens returns the max tokens specified in metadata, or fallback if not set
func (p *PromptDocument) GetMaxTokens(fallback int) int {
	if p.Metadata.MaxTokens != nil {
		return *p.Metadata.MaxTokens
	}
	return fallback
}
