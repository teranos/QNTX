package wasm

import (
	"encoding/json"
	"testing"
)

func TestParseAxQuery(t *testing.T) {
	engine, err := GetEngine()
	if err != nil {
		t.Fatalf("GetEngine: %v", err)
	}

	tests := []struct {
		name       string
		input      string
		wantSubj   []string
		wantPred   []string
		wantCtx    []string
		wantActors []string
	}{
		{
			name:     "subjects only",
			input:    "ALICE BOB",
			wantSubj: []string{"ALICE", "BOB"},
		},
		{
			name:     "subject with predicate",
			input:    "ALICE is author",
			wantSubj: []string{"ALICE"},
			wantPred: []string{"author"},
		},
		{
			name:       "full query",
			input:      "ALICE is author_of of GitHub by CHARLIE",
			wantSubj:   []string{"ALICE"},
			wantPred:   []string{"author_of"},
			wantCtx:    []string{"GitHub"},
			wantActors: []string{"CHARLIE"},
		},
		{
			name:  "empty query",
			input: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.Call("parse_ax_query", tt.input)
			if err != nil {
				t.Fatalf("Call: %v", err)
			}

			var parsed struct {
				Subjects   []string `json:"subjects"`
				Predicates []string `json:"predicates"`
				Contexts   []string `json:"contexts"`
				Actors     []string `json:"actors"`
				Error      string   `json:"error,omitempty"`
			}
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("unmarshal: %v (raw: %s)", err, result)
			}

			if parsed.Error != "" {
				t.Fatalf("parser error: %s", parsed.Error)
			}

			assertSliceEqual(t, "subjects", tt.wantSubj, parsed.Subjects)
			assertSliceEqual(t, "predicates", tt.wantPred, parsed.Predicates)
			assertSliceEqual(t, "contexts", tt.wantCtx, parsed.Contexts)
			assertSliceEqual(t, "actors", tt.wantActors, parsed.Actors)
		})
	}
}

func TestParseAxQueryTemporal(t *testing.T) {
	engine, err := GetEngine()
	if err != nil {
		t.Fatalf("GetEngine: %v", err)
	}

	result, err := engine.Call("parse_ax_query", "ALICE is author since 2024-01-01")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	temporal, ok := parsed["temporal"]
	if !ok || temporal == nil {
		t.Fatal("expected temporal clause")
	}

	tc, ok := temporal.(map[string]interface{})
	if !ok {
		t.Fatalf("expected temporal to be map, got %T", temporal)
	}

	since, ok := tc["Since"]
	if !ok {
		t.Fatal("expected Since in temporal")
	}
	if since != "2024-01-01" {
		t.Fatalf("expected Since=2024-01-01, got %v", since)
	}
}

func BenchmarkParseAxQuery(b *testing.B) {
	engine, err := GetEngine()
	if err != nil {
		b.Fatalf("GetEngine: %v", err)
	}

	input := "ALICE BOB is author_of of GitHub Linux by CHARLIE since 2024-01-01 so notify"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.Call("parse_ax_query", input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func assertSliceEqual(t *testing.T, name string, want, got []string) {
	t.Helper()
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if len(want) != len(got) {
		t.Errorf("%s: want %v, got %v", name, want, got)
		return
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("%s[%d]: want %q, got %q", name, i, want[i], got[i])
		}
	}
}
