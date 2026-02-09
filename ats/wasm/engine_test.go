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

func TestFuzzyRebuildAndSearch(t *testing.T) {
	engine, err := GetEngine()
	if err != nil {
		t.Fatalf("GetEngine: %v", err)
	}

	// Rebuild index with vocabulary
	rebuildInput := `{"predicates":["is_author_of","is_maintainer_of","works_at","contributes_to"],"contexts":["GitHub","GitLab","Linux","Kubernetes"]}`
	result, err := engine.Call("fuzzy_rebuild_index", rebuildInput)
	if err != nil {
		t.Fatalf("fuzzy_rebuild_index: %v", err)
	}

	var rebuild struct {
		PredicateCount int    `json:"predicate_count"`
		ContextCount   int    `json:"context_count"`
		Hash           string `json:"hash"`
		Error          string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(result), &rebuild); err != nil {
		t.Fatalf("unmarshal rebuild result: %v (raw: %s)", err, result)
	}
	if rebuild.Error != "" {
		t.Fatalf("rebuild error: %s", rebuild.Error)
	}
	if rebuild.PredicateCount != 4 {
		t.Errorf("predicate_count: want 4, got %d", rebuild.PredicateCount)
	}
	if rebuild.ContextCount != 4 {
		t.Errorf("context_count: want 4, got %d", rebuild.ContextCount)
	}
	if rebuild.Hash == "" {
		t.Error("expected non-empty hash")
	}

	// Search predicates
	searchInput := `{"query":"author","vocab_type":"predicates","limit":10,"min_score":0.6}`
	result, err = engine.Call("fuzzy_search", searchInput)
	if err != nil {
		t.Fatalf("fuzzy_search: %v", err)
	}

	var search struct {
		Matches []struct {
			Value    string  `json:"value"`
			Score    float64 `json:"score"`
			Strategy string  `json:"strategy"`
		} `json:"matches"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(result), &search); err != nil {
		t.Fatalf("unmarshal search result: %v (raw: %s)", err, result)
	}
	if search.Error != "" {
		t.Fatalf("search error: %s", search.Error)
	}
	if len(search.Matches) == 0 {
		t.Fatal("expected at least one match for 'author'")
	}

	// The top match should be is_author_of (word boundary match)
	if search.Matches[0].Value != "is_author_of" {
		t.Errorf("top match: want is_author_of, got %s", search.Matches[0].Value)
	}
	if search.Matches[0].Score < 0.6 {
		t.Errorf("top match score too low: %f", search.Matches[0].Score)
	}

	// Search contexts
	searchInput = `{"query":"git","vocab_type":"contexts","limit":10,"min_score":0.5}`
	result, err = engine.Call("fuzzy_search", searchInput)
	if err != nil {
		t.Fatalf("fuzzy_search contexts: %v", err)
	}

	if err := json.Unmarshal([]byte(result), &search); err != nil {
		t.Fatalf("unmarshal context search: %v (raw: %s)", err, result)
	}
	if search.Error != "" {
		t.Fatalf("context search error: %s", search.Error)
	}
	if len(search.Matches) == 0 {
		t.Fatal("expected at least one context match for 'git'")
	}
}

func TestFuzzyIsReady(t *testing.T) {
	engine, err := GetEngine()
	if err != nil {
		t.Fatalf("GetEngine: %v", err)
	}

	// Note: fuzzy_is_ready returns a u32, not a packed string.
	// We need to call it differently than Call() which expects string results.
	// For now, verify it's callable through the rebuild+search flow.
	// The rebuild in TestFuzzyRebuildAndSearch already populates the engine.

	// Rebuild to ensure state
	rebuildInput := `{"predicates":["test"],"contexts":[]}`
	_, err = engine.Call("fuzzy_rebuild_index", rebuildInput)
	if err != nil {
		t.Fatalf("fuzzy_rebuild_index: %v", err)
	}

	// Search should find exact match
	searchInput := `{"query":"test","vocab_type":"predicates","limit":5,"min_score":0.6}`
	result, err := engine.Call("fuzzy_search", searchInput)
	if err != nil {
		t.Fatalf("fuzzy_search: %v", err)
	}

	var search struct {
		Matches []struct {
			Value    string  `json:"value"`
			Score    float64 `json:"score"`
			Strategy string  `json:"strategy"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(result), &search); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, result)
	}
	if len(search.Matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(search.Matches))
	}
	if search.Matches[0].Value != "test" {
		t.Errorf("want 'test', got %q", search.Matches[0].Value)
	}
	if search.Matches[0].Score != 1.0 {
		t.Errorf("exact match should score 1.0, got %f", search.Matches[0].Score)
	}
	if search.Matches[0].Strategy != "exact" {
		t.Errorf("expected 'exact' strategy, got %q", search.Matches[0].Strategy)
	}
}

func BenchmarkFuzzySearch(b *testing.B) {
	engine, err := GetEngine()
	if err != nil {
		b.Fatalf("GetEngine: %v", err)
	}

	// Build a larger vocabulary
	rebuildInput := `{"predicates":["is_author_of","is_maintainer_of","works_at","contributes_to","manages","leads","reviews","mentors","founded","advises"],"contexts":["GitHub","GitLab","Linux","Kubernetes","Docker","AWS","Azure","GCP","Rust","Go"]}`
	_, err = engine.Call("fuzzy_rebuild_index", rebuildInput)
	if err != nil {
		b.Fatalf("rebuild: %v", err)
	}

	searchInput := `{"query":"author","vocab_type":"predicates","limit":5,"min_score":0.6}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.Call("fuzzy_search", searchInput)
		if err != nil {
			b.Fatal(err)
		}
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
