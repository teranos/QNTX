package attrs

import (
	"testing"
)

type promptAttrs struct {
	Template     string `attr:"template"`
	Version      int    `attr:"version"`
	SystemPrompt string `attr:"system_prompt,omitempty"`
	Model        string `attr:"model,omitempty"`
}

func TestScanBasic(t *testing.T) {
	m := map[string]any{
		"template": "Hello {{name}}",
		"version":  float64(3), // JSON numbers are float64
		"model":    "gpt-4",
	}

	var p promptAttrs
	Scan(m, &p)

	if p.Template != "Hello {{name}}" {
		t.Errorf("Template = %q, want %q", p.Template, "Hello {{name}}")
	}
	if p.Version != 3 {
		t.Errorf("Version = %d, want 3", p.Version)
	}
	if p.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", p.Model, "gpt-4")
	}
	if p.SystemPrompt != "" {
		t.Errorf("SystemPrompt = %q, want empty", p.SystemPrompt)
	}
}

func TestScanNilMap(t *testing.T) {
	var p promptAttrs
	Scan(nil, &p) // should not panic
	if p.Template != "" {
		t.Errorf("expected zero value")
	}
}

func TestFromBasic(t *testing.T) {
	p := promptAttrs{
		Template: "Hello",
		Version:  2,
		Model:    "gpt-4",
	}

	m := From(p)

	if m["template"] != "Hello" {
		t.Errorf("template = %v, want Hello", m["template"])
	}
	if m["version"] != 2 {
		t.Errorf("version = %v, want 2", m["version"])
	}
	if m["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", m["model"])
	}
}

func TestFromOmitempty(t *testing.T) {
	p := promptAttrs{
		Template: "Hello",
		Version:  1,
		// SystemPrompt and Model are zero → omitempty
	}

	m := From(p)

	if _, ok := m["system_prompt"]; ok {
		t.Error("system_prompt should be omitted when empty")
	}
	if _, ok := m["model"]; ok {
		t.Error("model should be omitted when empty")
	}
	// template and version are NOT omitempty, so version=1 is included
	if m["version"] != 1 {
		t.Errorf("version = %v, want 1", m["version"])
	}
}

type typeDefAttrs struct {
	Color            string   `attr:"display_color"`
	Label            string   `attr:"display_label"`
	Deprecated       bool     `attr:"deprecated"`
	Opacity          float64  `attr:"opacity"`
	RichStringFields []string `attr:"rich_string_fields,omitempty"`
	ArrayFields      []string `attr:"array_fields,omitempty"`
}

func TestScanSlice(t *testing.T) {
	m := map[string]any{
		"display_color":      "#3498db",
		"display_label":      "Commit",
		"deprecated":         false,
		"opacity":            0.8,
		"rich_string_fields": []any{"notes", "description"},
	}

	var td typeDefAttrs
	Scan(m, &td)

	if td.Color != "#3498db" {
		t.Errorf("Color = %q", td.Color)
	}
	if td.Opacity != 0.8 {
		t.Errorf("Opacity = %f", td.Opacity)
	}
	if len(td.RichStringFields) != 2 || td.RichStringFields[0] != "notes" {
		t.Errorf("RichStringFields = %v", td.RichStringFields)
	}
	if td.ArrayFields != nil {
		t.Errorf("ArrayFields should be nil, got %v", td.ArrayFields)
	}
}

func TestScanStringSlice(t *testing.T) {
	// When attributes come from Go code (not JSON), slices are already []string
	m := map[string]any{
		"rich_string_fields": []string{"content", "summary"},
	}

	var td typeDefAttrs
	Scan(m, &td)

	if len(td.RichStringFields) != 2 || td.RichStringFields[0] != "content" {
		t.Errorf("RichStringFields = %v", td.RichStringFields)
	}
}

func TestRoundtrip(t *testing.T) {
	original := promptAttrs{
		Template:     "Hello {{name}}",
		Version:      5,
		SystemPrompt: "Be helpful",
		Model:        "claude",
	}

	m := From(original)
	var back promptAttrs
	Scan(m, &back)

	if back != original {
		t.Errorf("roundtrip failed: got %+v, want %+v", back, original)
	}
}

type ptrAttrs struct {
	Opacity *float64 `attr:"opacity,omitempty"`
}

func TestScanPointerFloat(t *testing.T) {
	m := map[string]any{
		"opacity": 0.5,
	}

	var p ptrAttrs
	Scan(m, &p)

	if p.Opacity == nil || *p.Opacity != 0.5 {
		t.Errorf("Opacity = %v, want 0.5", p.Opacity)
	}
}

func TestScanPointerMissing(t *testing.T) {
	m := map[string]any{}

	var p ptrAttrs
	Scan(m, &p)

	if p.Opacity != nil {
		t.Errorf("Opacity should be nil, got %v", *p.Opacity)
	}
}

func TestFromDereferencesPointers(t *testing.T) {
	opacity := 0.5
	p := ptrAttrs{Opacity: &opacity}

	m := From(p)

	// Value should be float64, not *float64
	v, ok := m["opacity"].(float64)
	if !ok {
		t.Fatalf("opacity should be float64, got %T", m["opacity"])
	}
	if v != 0.5 {
		t.Errorf("opacity = %f, want 0.5", v)
	}
}

func TestFromSkipsNilPointers(t *testing.T) {
	p := ptrAttrs{Opacity: nil}

	m := From(p)

	if _, ok := m["opacity"]; ok {
		t.Error("nil pointer should not appear in map")
	}
}

func TestFromSkipsUntaggedFields(t *testing.T) {
	type mixed struct {
		Tagged   string `attr:"tagged"`
		Untagged string
	}

	m := From(mixed{Tagged: "yes", Untagged: "no"})

	if m["tagged"] != "yes" {
		t.Errorf("tagged = %v", m["tagged"])
	}
	if _, ok := m["Untagged"]; ok {
		t.Error("untagged field should not appear in map")
	}
}
