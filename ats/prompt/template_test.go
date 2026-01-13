package prompt

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		wantErr     bool
		placeholders []string
	}{
		{
			name:        "literal only",
			template:    "Hello world",
			wantErr:     false,
			placeholders: nil,
		},
		{
			name:        "single placeholder",
			template:    "Hello {{subject}}",
			wantErr:     false,
			placeholders: []string{"subject"},
		},
		{
			name:        "multiple placeholders",
			template:    "{{subject}} is {{predicate}} of {{context}}",
			wantErr:     false,
			placeholders: []string{"subject", "predicate", "context"},
		},
		{
			name:        "attribute access",
			template:    "Score: {{attributes.score}}",
			wantErr:     false,
			placeholders: []string{"attributes.score"},
		},
		{
			name:        "nested attribute",
			template:    "Value: {{attributes.data.nested.value}}",
			wantErr:     false,
			placeholders: []string{"attributes.data.nested.value"},
		},
		{
			name:        "all fields",
			template:    "{{id}} {{subjects}} {{predicates}} {{contexts}} {{actors}} {{temporal}} {{source}} {{attributes}}",
			wantErr:     false,
			placeholders: []string{"id", "subjects", "predicates", "contexts", "actors", "temporal", "source", "attributes"},
		},
		{
			name:     "empty template",
			template: "",
			wantErr:  true,
		},
		{
			name:     "invalid field",
			template: "{{invalid_field}}",
			wantErr:  true,
		},
		{
			name:     "empty attribute path",
			template: "{{attributes.}}",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := Parse(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			got := tmpl.GetPlaceholders()
			if len(got) != len(tt.placeholders) {
				t.Errorf("GetPlaceholders() = %v, want %v", got, tt.placeholders)
			}
			for i, p := range got {
				if i < len(tt.placeholders) && p != tt.placeholders[i] {
					t.Errorf("placeholder[%d] = %v, want %v", i, p, tt.placeholders[i])
				}
			}
		})
	}
}

func TestExecute(t *testing.T) {
	timestamp := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

	testAs := &types.As{
		ID:         "AS123",
		Subjects:   []string{"ALICE"},
		Predicates: []string{"speaks"},
		Contexts:   []string{"english", "spanish"},
		Actors:     []string{"system"},
		Timestamp:  timestamp,
		Source:     "test",
		Attributes: map[string]interface{}{
			"score":    85.5,
			"verified": true,
			"count":    42,
			"nested": map[string]interface{}{
				"value": "deep",
			},
		},
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "literal only",
			template: "Hello world",
			want:     "Hello world",
		},
		{
			name:     "single subject",
			template: "{{subject}} says hello",
			want:     "ALICE says hello",
		},
		{
			name:     "multiple contexts joined",
			template: "Speaks {{context}}",
			want:     "Speaks english, spanish",
		},
		{
			name:     "contexts as JSON array",
			template: "Languages: {{contexts}}",
			want:     `Languages: ["english","spanish"]`,
		},
		{
			name:     "temporal as ISO8601",
			template: "At {{temporal}}",
			want:     "At 2024-06-15T10:30:00Z",
		},
		{
			name:     "attribute number",
			template: "Score: {{attributes.score}}",
			want:     "Score: 85.5",
		},
		{
			name:     "attribute boolean",
			template: "Verified: {{attributes.verified}}",
			want:     "Verified: true",
		},
		{
			name:     "attribute integer",
			template: "Count: {{attributes.count}}",
			want:     "Count: 42",
		},
		{
			name:     "nested attribute",
			template: "Value: {{attributes.nested.value}}",
			want:     "Value: deep",
		},
		{
			name:     "missing attribute",
			template: "Missing: {{attributes.nonexistent}}",
			want:     "Missing: ",
		},
		{
			name:     "full sentence",
			template: "{{subject}} {{predicate}} {{context}} (verified: {{attributes.verified}})",
			want:     "ALICE speaks english, spanish (verified: true)",
		},
		{
			name:     "id field",
			template: "Attestation: {{id}}",
			want:     "Attestation: AS123",
		},
		{
			name:     "source field",
			template: "From: {{source}}",
			want:     "From: test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := Parse(tt.template)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			got, err := tmpl.Execute(testAs)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecuteNilAttestation(t *testing.T) {
	tmpl, err := Parse("{{subject}}")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	_, err = tmpl.Execute(nil)
	if err == nil {
		t.Error("Execute(nil) should return error")
	}
}

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		wantErr  bool
	}{
		{"valid simple", "Hello {{subject}}", false},
		{"valid complex", "{{subject}} is {{predicate}} of {{context}}", false},
		{"invalid field", "{{unknown}}", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTemplate(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTemplate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRaw(t *testing.T) {
	raw := "Hello {{subject}}"
	tmpl, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if tmpl.Raw() != raw {
		t.Errorf("Raw() = %q, want %q", tmpl.Raw(), raw)
	}
}
