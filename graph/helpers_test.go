package graph

import (
	"testing"
)

// TestNormalizeNodeID tests ID normalization
func TestNormalizeNodeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"UPPERCASE", "uppercase"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"with spaces", "with_spaces"},
		{"special@chars#here", "special_chars_here"},
		{"email@example.com", "email_example_com"},
		{"dots.and.stuff", "dots_and_stuff"},
		{"123numbers", "123numbers"},
		{"", ""},
	}

	for _, tt := range tests {
		result := normalizeNodeID(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeNodeID(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestIsLiteralValue tests literal value detection
func TestIsLiteralValue(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		// Numeric values
		{"123", true},
		{"45.67", true},
		{"0", true},

		// Boolean values
		{"true", true},
		{"false", true},
		{"True", true},
		{"FALSE", true},

		// Email patterns
		{"user@example.com", true},
		{"test@test.org", true},

		// Phone patterns
		{"+1-555-1234", true},
		{"0123-456-789", true},

		// Years experience
		{"5 years", true},
		{"3y", true},
		{"10years", true},

		// Short values
		{"Go", true},
		{"JS", true},
		{"C", true},

		// Non-literal values (proper entities)
		{"Amsterdam", false},
		{"Software Engineer", false},
		{"Acme Corporation", false},
		{"Rust", false},
		{"Java", false},
	}

	for _, tt := range tests {
		result := isLiteralValue(tt.value)
		if result != tt.expected {
			t.Errorf("isLiteralValue(%q) = %v, want %v", tt.value, result, tt.expected)
		}
	}
}
