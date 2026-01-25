package prompt

import (
	"testing"

	"github.com/teranos/QNTX/ats/types"
)

func TestParseAction(t *testing.T) {
	tests := []struct {
		name     string
		filter   *types.AxFilter
		want     *Action
		wantErr  bool
		wantNil  bool
	}{
		{
			name:    "nil filter",
			filter:  nil,
			wantNil: true,
		},
		{
			name:    "empty so_actions",
			filter:  &types.AxFilter{},
			wantNil: true,
		},
		{
			name: "non-prompt action",
			filter: &types.AxFilter{
				SoActions: []string{"csv"},
			},
			wantNil: true,
		},
		{
			name: "prompt with no template",
			filter: &types.AxFilter{
				SoActions: []string{"prompt"},
			},
			wantErr: true,
		},
		{
			name: "simple template",
			filter: &types.AxFilter{
				SoActions: []string{"prompt", "Summarize:", "{{subject}}"},
			},
			want: &Action{
				Template: "Summarize: {{subject}}",
			},
		},
		{
			name: "quoted template",
			filter: &types.AxFilter{
				SoActions: []string{"prompt", `"Analyze {{subject}} in context {{context}}"`},
			},
			want: &Action{
				Template: "Analyze {{subject}} in context {{context}}",
			},
		},
		{
			name: "template with system prompt",
			filter: &types.AxFilter{
				SoActions: []string{"prompt", "{{subject}}", "with", "You", "are", "helpful"},
			},
			want: &Action{
				Template:     "{{subject}}",
				SystemPrompt: "You are helpful",
			},
		},
		{
			name: "template with model",
			filter: &types.AxFilter{
				SoActions: []string{"prompt", "{{subject}}", "model", "gpt-4"},
			},
			want: &Action{
				Template: "{{subject}}",
				Model:    "gpt-4",
			},
		},
		{
			name: "template with provider",
			filter: &types.AxFilter{
				SoActions: []string{"prompt", "{{subject}}", "provider", "local"},
			},
			want: &Action{
				Template: "{{subject}}",
				Provider: "local",
			},
		},
		{
			name: "full options",
			filter: &types.AxFilter{
				SoActions: []string{
					"prompt", "{{subject}}", "is", "{{predicate}}",
					"with", "Be", "concise",
					"model", "gpt-4",
					"provider", "openrouter",
					"predicate", "analysis",
				},
			},
			want: &Action{
				Template:        "{{subject}} is {{predicate}}",
				SystemPrompt:    "Be concise",
				Model:           "gpt-4",
				Provider:        "openrouter",
				ResultPredicate: "analysis",
			},
		},
		{
			name: "invalid template placeholder",
			filter: &types.AxFilter{
				SoActions: []string{"prompt", "{{invalid_field}}"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAction(tt.filter)

			if tt.wantErr {
				if err == nil {
					t.Error("ParseAction() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseAction() unexpected error: %v", err)
				return
			}

			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseAction() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Fatal("ParseAction() = nil, want non-nil")
			}

			if got.Template != tt.want.Template {
				t.Errorf("Template = %q, want %q", got.Template, tt.want.Template)
			}
			if got.SystemPrompt != tt.want.SystemPrompt {
				t.Errorf("SystemPrompt = %q, want %q", got.SystemPrompt, tt.want.SystemPrompt)
			}
			if got.Model != tt.want.Model {
				t.Errorf("Model = %q, want %q", got.Model, tt.want.Model)
			}
			if got.Provider != tt.want.Provider {
				t.Errorf("Provider = %q, want %q", got.Provider, tt.want.Provider)
			}
			if got.ResultPredicate != tt.want.ResultPredicate {
				t.Errorf("ResultPredicate = %q, want %q", got.ResultPredicate, tt.want.ResultPredicate)
			}
		})
	}
}

func TestIsPromptAction(t *testing.T) {
	tests := []struct {
		name   string
		filter *types.AxFilter
		want   bool
	}{
		{"nil filter", nil, false},
		{"empty filter", &types.AxFilter{}, false},
		{"csv action", &types.AxFilter{SoActions: []string{"csv"}}, false},
		{"prompt action", &types.AxFilter{SoActions: []string{"prompt", "{{subject}}"}}, true},
		{"PROMPT uppercase", &types.AxFilter{SoActions: []string{"PROMPT", "{{subject}}"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPromptAction(tt.filter); got != tt.want {
				t.Errorf("IsPromptAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToPayload(t *testing.T) {
	action := &Action{
		Template:        "{{subject}} is {{predicate}}",
		SystemPrompt:    "Be helpful",
		Provider:        "local",
		Model:           "llama2",
		ResultPredicate: "summary",
	}

	filter := types.AxFilter{
		Subjects:   []string{"ALICE"},
		Predicates: []string{"speaks"},
		SoActions:  []string{"prompt", "{{subject}}"},
	}

	soPayload, err := action.ToPayload(filter)
	if err != nil {
		t.Fatalf("ToPayload() error: %v", err)
	}

	payload, ok := soPayload.(*Payload)
	if !ok {
		t.Fatalf("ToPayload() returned wrong type: %T", soPayload)
	}

	if payload.Template != action.Template {
		t.Errorf("Template = %q, want %q", payload.Template, action.Template)
	}
	if payload.SystemPrompt != action.SystemPrompt {
		t.Errorf("SystemPrompt = %q, want %q", payload.SystemPrompt, action.SystemPrompt)
	}
	if payload.Provider != action.Provider {
		t.Errorf("Provider = %q, want %q", payload.Provider, action.Provider)
	}
	if payload.Model != action.Model {
		t.Errorf("Model = %q, want %q", payload.Model, action.Model)
	}
	if payload.ResultPredicate != action.ResultPredicate {
		t.Errorf("ResultPredicate = %q, want %q", payload.ResultPredicate, action.ResultPredicate)
	}

	// SoActions should be cleared in the embedded filter
	if len(payload.AxFilter.SoActions) != 0 {
		t.Errorf("AxFilter.SoActions = %v, want empty", payload.AxFilter.SoActions)
	}

	// Original filter data should be preserved
	if len(payload.AxFilter.Subjects) != 1 || payload.AxFilter.Subjects[0] != "ALICE" {
		t.Errorf("AxFilter.Subjects = %v, want [ALICE]", payload.AxFilter.Subjects)
	}
}

func TestToPayloadJSON(t *testing.T) {
	action := &Action{
		Template: "{{subject}}",
	}

	filter := types.AxFilter{
		Subjects: []string{"BOB"},
	}

	data, err := action.ToPayloadJSON(filter)
	if err != nil {
		t.Fatalf("ToPayloadJSON() error: %v", err)
	}

	if len(data) == 0 {
		t.Error("ToPayloadJSON() returned empty data")
	}
}
