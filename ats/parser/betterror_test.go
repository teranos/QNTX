//go:build !qntxwasm

package parser

import (
	"strings"
	"testing"
)

// TestBETTERROR_StructuredErrorFields verifies that ParseError contains expected metadata
func TestBETTERROR_StructuredErrorFields(t *testing.T) {
	err := NewParseError(ErrorKindTemporal, "invalid date format").
		WithPosition(5, 10).
		WithSuggestion("Use ISO format").
		WithSuggestion("Or relative time").
		WithContext("query", "test query")

	if err.Kind != ErrorKindTemporal {
		t.Errorf("Expected ErrorKindTemporal, got %v", err.Kind)
	}

	if err.Severity != SeverityError {
		t.Errorf("Expected SeverityError, got %v", err.Severity)
	}

	if err.Position != 5 {
		t.Errorf("Expected position 5, got %d", err.Position)
	}

	if len(err.Suggestions) != 2 {
		t.Errorf("Expected 2 suggestions, got %d", len(err.Suggestions))
	}

	if err.Context["query"] != "test query" {
		t.Errorf("Expected context to contain query")
	}
}

// TestBETTERROR_TemporalErrorWithSuggestions verifies temporal errors include helpful suggestions
func TestBETTERROR_TemporalErrorWithSuggestions(t *testing.T) {
	query := []string{"ALICE", "is", "engineer", "since", "invalid_date"}

	_, err := ParseAxCommandWithContext(query, 0, ErrorContextPlain)
	if err == nil {
		t.Fatal("Expected error for invalid temporal expression")
	}

	// Temporal errors are best-effort, so they come wrapped in ParseWarning
	// Extract the warning message which contains the formatted ParseError
	pw, ok := err.(*ParseWarning)
	if !ok {
		t.Fatalf("Expected ParseWarning (best-effort), got %T: %v", err, err)
	}

	if len(pw.Warnings) == 0 {
		t.Fatal("Expected warnings in ParseWarning")
	}

	// The warning message should contain the structured error information
	warningMsg := pw.Warnings[0]

	// Verify it mentions temporal expression failure
	if !strings.Contains(warningMsg, "temporal expression") {
		t.Error("Warning should mention temporal expression failure")
	}

	// Verify suggestions appear in warning message (these come from ParseError.FormatError)
	if !strings.Contains(warningMsg, "ISO date format") {
		t.Error("Warning should contain ISO date format suggestion")
	}

	if !strings.Contains(warningMsg, "relative time") {
		t.Error("Warning should contain relative time suggestion")
	}

	if !strings.Contains(warningMsg, "named days") {
		t.Error("Warning should contain named days suggestion")
	}

	// Verify plain format is clean (no ANSI codes)
	if strings.Contains(warningMsg, "\x1b[") {
		t.Error("Plain context warnings should not contain ANSI codes")
	}

	t.Logf("✓ BETTERROR temporal error with suggestions (best-effort warning)")
	t.Logf("  Warning message: %s", warningMsg[:min(200, len(warningMsg))])
}

// TestBETTERROR_LSPDiagnosticConversion verifies conversion to LSP diagnostic format
func TestBETTERROR_LSPDiagnosticConversion(t *testing.T) {
	err := NewParseError(ErrorKindSyntax, "unexpected token").
		WithPosition(3, 5).
		WithSuggestion("Check query syntax")

	diagData := err.ToLSPDiagnostic()

	// Verify fields exist
	if diagData["severity"] != "error" {
		t.Errorf("Expected severity 'error', got %v", diagData["severity"])
	}

	if diagData["kind"] != "syntax" {
		t.Errorf("Expected kind 'syntax', got %v", diagData["kind"])
	}

	message, ok := diagData["message"].(string)
	if !ok {
		t.Fatal("Expected message to be string")
	}

	if !strings.Contains(message, "unexpected token") {
		t.Error("Message should contain error text")
	}

	suggestions, ok := diagData["suggestions"].([]string)
	if !ok || len(suggestions) != 1 {
		t.Error("Expected suggestions array with 1 item")
	}

	t.Logf("✓ BETTERROR converts to LSP diagnostic format")
}

// TestBETTERROR_BestEffortParsing verifies temporal errors become warnings (non-fatal)
func TestBETTERROR_BestEffortParsing(t *testing.T) {
	// Query with invalid temporal expression
	query := []string{"ALICE", "is", "engineer", "until", "bad_date"}

	filter, err := ParseAxCommandWithContext(query, 0, ErrorContextPlain)
	if err == nil {
		t.Fatal("Expected warning for invalid temporal expression")
	}

	// Should be ParseWarning (best-effort), not hard error
	pw, ok := err.(*ParseWarning)
	if !ok {
		t.Fatalf("Expected ParseWarning (best-effort), got %T", err)
	}

	// Filter should still be returned with partial results
	if filter == nil {
		t.Error("Best-effort parsing should return filter with partial results")
	}

	// Should have subject and predicate parsed successfully
	if len(filter.Subjects) == 0 || len(filter.Predicates) == 0 {
		t.Error("Best-effort parsing should have parsed subjects and predicates")
	}

	// Warning message should not contain ANSI codes (plain context)
	if strings.Contains(pw.Error(), "\x1b[") {
		t.Error("Plain context warnings should not have ANSI codes")
	}

	t.Logf("✓ Best-effort parsing: filter has %d subjects, %d predicates, with %d warnings",
		len(filter.Subjects), len(filter.Predicates), len(pw.Warnings))
}

// TestBETTERROR_ErrorKinds verifies different error kinds
func TestBETTERROR_ErrorKinds(t *testing.T) {
	kinds := []ErrorKind{
		ErrorKindSyntax,
		ErrorKindSemantic,
		ErrorKindTemporal,
		ErrorKindContext,
		ErrorKindUnknown,
	}

	for _, kind := range kinds {
		err := NewParseError(kind, "test error")
		if err.Kind != kind {
			t.Errorf("Expected kind %v, got %v", kind, err.Kind)
		}
	}

	t.Logf("✓ All %d error kinds work correctly", len(kinds))
}

// TestBETTERROR_SeverityLevels verifies severity levels affect formatting
func TestBETTERROR_SeverityLevels(t *testing.T) {
	severities := []ErrorSeverity{
		SeverityError,
		SeverityWarning,
		SeverityInfo,
		SeverityHint,
	}

	for _, severity := range severities {
		err := NewParseError(ErrorKindUnknown, "test message").
			WithSeverity(severity)

		if err.Severity != severity {
			t.Errorf("Expected severity %v, got %v", severity, err.Severity)
		}

		// Terminal format should use different colors for different severities
		terminalMsg := err.FormatError(ErrorContextTerminal)
		if !strings.Contains(terminalMsg, "\x1b[") {
			t.Errorf("Terminal format for %v should have ANSI codes", severity)
		}

		// Check IsWarning helper
		if severity == SeverityWarning {
			if !err.IsWarning() {
				t.Error("IsWarning() should return true for SeverityWarning")
			}
		} else {
			if err.IsWarning() {
				t.Error("IsWarning() should return false for non-warning severities")
			}
		}
	}

	t.Logf("✓ All %d severity levels work correctly", len(severities))
}
