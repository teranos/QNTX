//go:build cgo && rustparser

package axparser

import (
	"testing"
)

func TestParseSimpleQuery(t *testing.T) {
	result, err := Parse("ALICE is author")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Subjects) != 1 || result.Subjects[0] != "ALICE" {
		t.Errorf("expected subjects [ALICE], got %v", result.Subjects)
	}

	if len(result.Predicates) != 1 || result.Predicates[0] != "author" {
		t.Errorf("expected predicates [author], got %v", result.Predicates)
	}
}

func TestParseFullQuery(t *testing.T) {
	result, err := Parse("ALICE BOB is author_of of GitHub by CHARLIE since 2024-01-01 so notify")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check subjects
	if len(result.Subjects) != 2 {
		t.Errorf("expected 2 subjects, got %d", len(result.Subjects))
	}
	if result.Subjects[0] != "ALICE" || result.Subjects[1] != "BOB" {
		t.Errorf("expected subjects [ALICE, BOB], got %v", result.Subjects)
	}

	// Check predicates
	if len(result.Predicates) != 1 || result.Predicates[0] != "author_of" {
		t.Errorf("expected predicates [author_of], got %v", result.Predicates)
	}

	// Check contexts
	if len(result.Contexts) != 1 || result.Contexts[0] != "GitHub" {
		t.Errorf("expected contexts [GitHub], got %v", result.Contexts)
	}

	// Check actors
	if len(result.Actors) != 1 || result.Actors[0] != "CHARLIE" {
		t.Errorf("expected actors [CHARLIE], got %v", result.Actors)
	}

	// Check temporal
	if result.Temporal == nil {
		t.Error("expected temporal clause")
	} else {
		if result.Temporal.Type != TemporalSince {
			t.Errorf("expected TemporalSince, got %d", result.Temporal.Type)
		}
		if result.Temporal.Start != "2024-01-01" {
			t.Errorf("expected start 2024-01-01, got %s", result.Temporal.Start)
		}
	}

	// Check actions
	if len(result.Actions) != 1 || result.Actions[0] != "notify" {
		t.Errorf("expected actions [notify], got %v", result.Actions)
	}
}

func TestParseTemporalBetween(t *testing.T) {
	result, err := Parse("ALICE is author between 2024-01-01 and 2024-12-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Temporal == nil {
		t.Fatal("expected temporal clause")
	}

	if result.Temporal.Type != TemporalBetween {
		t.Errorf("expected TemporalBetween, got %d", result.Temporal.Type)
	}

	if result.Temporal.Start != "2024-01-01" {
		t.Errorf("expected start 2024-01-01, got %s", result.Temporal.Start)
	}

	if result.Temporal.End != "2024-12-31" {
		t.Errorf("expected end 2024-12-31, got %s", result.Temporal.End)
	}
}

func TestParseTemporalOver(t *testing.T) {
	result, err := Parse("ALICE is experienced over 5y")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Temporal == nil {
		t.Fatal("expected temporal clause")
	}

	if result.Temporal.Type != TemporalOver {
		t.Errorf("expected TemporalOver, got %d", result.Temporal.Type)
	}

	if result.Temporal.DurationValue != 5.0 {
		t.Errorf("expected duration value 5.0, got %f", result.Temporal.DurationValue)
	}

	if result.Temporal.DurationUnit != DurationYears {
		t.Errorf("expected DurationYears, got %d", result.Temporal.DurationUnit)
	}
}

func TestParseEmptyQuery(t *testing.T) {
	result, err := Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.IsEmpty() {
		t.Error("expected empty result")
	}
}

func TestParseError(t *testing.T) {
	_, err := Parse("ALICE is")
	if err == nil {
		t.Error("expected error for incomplete query")
	}
}

func TestParseQuotedStrings(t *testing.T) {
	result, err := Parse("'John Doe' is 'senior developer' of 'ACME Corp'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Subjects) != 1 || result.Subjects[0] != "John Doe" {
		t.Errorf("expected subjects [John Doe], got %v", result.Subjects)
	}

	if len(result.Predicates) != 1 || result.Predicates[0] != "senior developer" {
		t.Errorf("expected predicates [senior developer], got %v", result.Predicates)
	}

	if len(result.Contexts) != 1 || result.Contexts[0] != "ACME Corp" {
		t.Errorf("expected contexts [ACME Corp], got %v", result.Contexts)
	}
}

func TestParseResultHelpers(t *testing.T) {
	result, _ := Parse("ALICE is author of GitHub")

	if !result.HasSubjects() {
		t.Error("expected HasSubjects() to be true")
	}
	if !result.HasPredicates() {
		t.Error("expected HasPredicates() to be true")
	}
	if !result.HasContexts() {
		t.Error("expected HasContexts() to be true")
	}
	if result.HasActors() {
		t.Error("expected HasActors() to be false")
	}
	if result.HasTemporal() {
		t.Error("expected HasTemporal() to be false")
	}
	if result.HasActions() {
		t.Error("expected HasActions() to be false")
	}
	if result.IsEmpty() {
		t.Error("expected IsEmpty() to be false")
	}
}

func BenchmarkParse(b *testing.B) {
	query := "ALICE BOB is author_of of GitHub Linux by CHARLIE since 2024-01-01 so notify"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParseSimple(b *testing.B) {
	query := "ALICE is author"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}
