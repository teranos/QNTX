package parser

import (
	"testing"
)

func TestParseAxQuery_SimpleQuery(t *testing.T) {
	filter, err := ParseAxQuery("ALICE is author")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(filter.Subjects) != 1 || filter.Subjects[0] != "ALICE" {
		t.Errorf("expected subjects [ALICE], got %v", filter.Subjects)
	}

	if len(filter.Predicates) != 1 || filter.Predicates[0] != "author" {
		t.Errorf("expected predicates [author], got %v", filter.Predicates)
	}
}

func TestParseAxQuery_FullQuery(t *testing.T) {
	filter, err := ParseAxQuery("ALICE BOB is author_of of GitHub by CHARLIE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check subjects (should be uppercase)
	if len(filter.Subjects) != 2 {
		t.Errorf("expected 2 subjects, got %d: %v", len(filter.Subjects), filter.Subjects)
	}

	// Check predicates
	if len(filter.Predicates) != 1 || filter.Predicates[0] != "author_of" {
		t.Errorf("expected predicates [author_of], got %v", filter.Predicates)
	}

	// Check contexts (should be lowercase)
	if len(filter.Contexts) != 1 {
		t.Errorf("expected 1 context, got %d: %v", len(filter.Contexts), filter.Contexts)
	}

	// Check actors (should be lowercase)
	if len(filter.Actors) != 1 {
		t.Errorf("expected 1 actor, got %d: %v", len(filter.Actors), filter.Actors)
	}
}

func TestParseAxQuery_TemporalSince(t *testing.T) {
	filter, err := ParseAxQuery("ALICE is author since 2024-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filter.TimeStart == nil {
		t.Error("expected TimeStart to be set")
	}
}

func TestParseAxQuery_TemporalBetween(t *testing.T) {
	filter, err := ParseAxQuery("ALICE is author between 2024-01-01 and 2024-12-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filter.TimeStart == nil {
		t.Error("expected TimeStart to be set")
	}
	if filter.TimeEnd == nil {
		t.Error("expected TimeEnd to be set")
	}
}

func TestParseAxQuery_TemporalOver(t *testing.T) {
	filter, err := ParseAxQuery("ALICE is experienced over 5y")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filter.OverComparison == nil {
		t.Fatal("expected OverComparison to be set")
	}

	if filter.OverComparison.Value != 5.0 {
		t.Errorf("expected OverComparison.Value = 5.0, got %f", filter.OverComparison.Value)
	}

	if filter.OverComparison.Unit != "y" {
		t.Errorf("expected OverComparison.Unit = 'y', got '%s'", filter.OverComparison.Unit)
	}
}

func TestParseAxQuery_QuotedStrings(t *testing.T) {
	filter, err := ParseAxQuery("'John Doe' is 'senior developer' of 'ACME Corp'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check subjects - quoted strings should preserve case but be uppercased
	if len(filter.Subjects) != 1 {
		t.Errorf("expected 1 subject, got %d: %v", len(filter.Subjects), filter.Subjects)
	}

	// Check predicates
	if len(filter.Predicates) != 1 {
		t.Errorf("expected 1 predicate, got %d: %v", len(filter.Predicates), filter.Predicates)
	}

	// Check contexts
	if len(filter.Contexts) != 1 {
		t.Errorf("expected 1 context, got %d: %v", len(filter.Contexts), filter.Contexts)
	}
}

func TestParseAxQuery_EmptyQuery(t *testing.T) {
	filter, err := ParseAxQuery("")
	if err != nil {
		// Empty query might return a warning, but should still return a filter
		if filter == nil {
			t.Fatalf("expected filter even with warning, got nil")
		}
	}

	// Filter should have defaults
	if filter.Limit != 100 {
		t.Errorf("expected default limit 100, got %d", filter.Limit)
	}
}

func TestParseAxQuery_SoAction(t *testing.T) {
	filter, err := ParseAxQuery("ALICE is author so notify")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(filter.SoActions) != 1 || filter.SoActions[0] != "notify" {
		t.Errorf("expected SoActions [notify], got %v", filter.SoActions)
	}
}

func TestTokenizeQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"ALICE is author", []string{"ALICE", "is", "author"}},
		{"'John Doe' is author", []string{"'John Doe'", "is", "author"}},
		{"ALICE is 'senior developer'", []string{"ALICE", "is", "'senior developer'"}},
		{"", nil},
		{"single", []string{"single"}},
	}

	for _, tt := range tests {
		result := tokenizeQuery(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("tokenizeQuery(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("tokenizeQuery(%q)[%d] = %q, want %q", tt.input, i, result[i], tt.expected[i])
			}
		}
	}
}

func TestRustParserAvailable(t *testing.T) {
	// Just log whether Rust parser is available
	t.Logf("RustParserAvailable = %v", RustParserAvailable)
}

func BenchmarkParseAxQuery(b *testing.B) {
	query := "ALICE BOB is author_of of GitHub Linux by CHARLIE since 2024-01-01 so notify"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseAxQuery(query)
	}
}

func BenchmarkParseAxQuery_Simple(b *testing.B) {
	query := "ALICE is author"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseAxQuery(query)
	}
}
