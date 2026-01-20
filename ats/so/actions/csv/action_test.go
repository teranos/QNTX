package csv

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
			name: "non-csv action",
			filter: &types.AxFilter{
				SoActions: []string{"prompt"},
			},
			wantNil: true,
		},
		{
			name: "csv with no filename",
			filter: &types.AxFilter{
				SoActions: []string{"csv"},
			},
			wantErr: true,
		},
		{
			name: "simple filename",
			filter: &types.AxFilter{
				SoActions: []string{"csv", "output.csv"},
			},
			want: &Action{
				Filename:  "output.csv",
				Delimiter: ",",
			},
		},
		{
			name: "quoted filename",
			filter: &types.AxFilter{
				SoActions: []string{"csv", `"output.csv"`},
			},
			want: &Action{
				Filename:  "output.csv",
				Delimiter: ",",
			},
		},
		{
			name: "filename with delimiter",
			filter: &types.AxFilter{
				SoActions: []string{"csv", "output.csv", "delimiter", ";"},
			},
			want: &Action{
				Filename:  "output.csv",
				Delimiter: ";",
			},
		},
		{
			name: "filename with headers",
			filter: &types.AxFilter{
				SoActions: []string{"csv", "output.csv", "headers", "id,subject,predicate"},
			},
			want: &Action{
				Filename:  "output.csv",
				Delimiter: ",",
				Headers:   []string{"id", "subject", "predicate"},
			},
		},
		{
			name: "full options",
			filter: &types.AxFilter{
				SoActions: []string{
					"csv", "data.csv",
					"delimiter", "|",
					"headers", "id,subject,predicate,context",
				},
			},
			want: &Action{
				Filename:  "data.csv",
				Delimiter: "|",
				Headers:   []string{"id", "subject", "predicate", "context"},
			},
		},
		{
			name: "headers with spaces",
			filter: &types.AxFilter{
				SoActions: []string{"csv", "output.csv", "headers", "id, subject, predicate"},
			},
			want: &Action{
				Filename:  "output.csv",
				Delimiter: ",",
				Headers:   []string{"id", "subject", "predicate"},
			},
		},
		{
			name: "multi-character delimiter",
			filter: &types.AxFilter{
				SoActions: []string{"csv", "output.csv", "delimiter", "||"},
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

			if got.Filename != tt.want.Filename {
				t.Errorf("Filename = %q, want %q", got.Filename, tt.want.Filename)
			}
			if got.Delimiter != tt.want.Delimiter {
				t.Errorf("Delimiter = %q, want %q", got.Delimiter, tt.want.Delimiter)
			}
			if len(got.Headers) != len(tt.want.Headers) {
				t.Errorf("Headers length = %d, want %d", len(got.Headers), len(tt.want.Headers))
			} else {
				for i := range got.Headers {
					if got.Headers[i] != tt.want.Headers[i] {
						t.Errorf("Headers[%d] = %q, want %q", i, got.Headers[i], tt.want.Headers[i])
					}
				}
			}
		})
	}
}

func TestIsCsvAction(t *testing.T) {
	tests := []struct {
		name   string
		filter *types.AxFilter
		want   bool
	}{
		{"nil filter", nil, false},
		{"empty filter", &types.AxFilter{}, false},
		{"prompt action", &types.AxFilter{SoActions: []string{"prompt", "{{subject}}"}}, false},
		{"csv action", &types.AxFilter{SoActions: []string{"csv", "output.csv"}}, true},
		{"CSV uppercase", &types.AxFilter{SoActions: []string{"CSV", "output.csv"}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCsvAction(tt.filter); got != tt.want {
				t.Errorf("IsCsvAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToPayload(t *testing.T) {
	action := &Action{
		Filename:  "output.csv",
		Delimiter: ";",
		Headers:   []string{"id", "subject"},
	}

	filter := types.AxFilter{
		Subjects:  []string{"ALICE"},
		SoActions: []string{"csv", "output.csv"},
	}

	soPayload, err := action.ToPayload(filter)
	if err != nil {
		t.Fatalf("ToPayload() error: %v", err)
	}

	payload, ok := soPayload.(*Payload)
	if !ok {
		t.Fatalf("ToPayload() returned wrong type: %T", soPayload)
	}

	if payload.Filename != action.Filename {
		t.Errorf("Filename = %q, want %q", payload.Filename, action.Filename)
	}
	if payload.Delimiter != action.Delimiter {
		t.Errorf("Delimiter = %q, want %q", payload.Delimiter, action.Delimiter)
	}
	if len(payload.Headers) != len(action.Headers) {
		t.Errorf("Headers length = %d, want %d", len(payload.Headers), len(action.Headers))
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
		Filename: "output.csv",
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
