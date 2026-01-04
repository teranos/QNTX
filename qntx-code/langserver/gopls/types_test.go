package gopls

import (
	"encoding/json"
	"testing"
)

// These tests don't require gopls binary - they test type conversions and parsing logic

func TestHover_GetText_WithMarkupContent(t *testing.T) {
	// Test parsing hover content in MarkupContent format (most common from gopls)
	hover := &Hover{
		Contents: json.RawMessage(`{"kind":"markdown","value":"func greet(name string) string"}`),
	}

	text := hover.GetText()

	expected := "func greet(name string) string"
	if text != expected {
		t.Errorf("GetText() = %q, want %q", text, expected)
	}
}

func TestHover_GetText_WithPlainString(t *testing.T) {
	// Test parsing hover content as plain string (fallback format)
	hover := &Hover{
		Contents: json.RawMessage(`"This is a plain string hover"`),
	}

	text := hover.GetText()

	expected := "This is a plain string hover"
	if text != expected {
		t.Errorf("GetText() = %q, want %q", text, expected)
	}
}

func TestHover_GetText_WithNilOrEmpty(t *testing.T) {
	// Test that empty hover returns empty string (defensive programming)
	tests := []struct {
		name  string
		hover *Hover
	}{
		{"nil hover", nil},
		{"empty contents", &Hover{Contents: json.RawMessage{}}},
		{"nil contents", &Hover{Contents: nil}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := tt.hover.GetText()
			if text != "" {
				t.Errorf("GetText() for %s = %q, want empty string", tt.name, text)
			}
		})
	}
}
