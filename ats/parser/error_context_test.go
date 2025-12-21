package parser

import (
	"strings"
	"testing"
)

// TestErrorContext_TerminalVsPlain verifies that errors are formatted correctly for each context
func TestErrorContext_TerminalVsPlain(t *testing.T) {
	// Query that will cause a temporal parsing error
	query := []string{"ALICE", "is", "engineer", "since", "invalid_date_xyz"}

	// Parse with Terminal context (should have ANSI codes)
	_, errTerminal := ParseAskCommandWithContext(query, 0, ErrorContextTerminal)
	if errTerminal == nil {
		t.Fatal("Expected error for invalid temporal expression")
	}

	// Parse with Plain context (should NOT have ANSI codes)
	_, errPlain := ParseAskCommandWithContext(query, 0, ErrorContextPlain)
	if errPlain == nil {
		t.Fatal("Expected error for invalid temporal expression")
	}

	terminalMsg := errTerminal.Error()
	plainMsg := errPlain.Error()

	// Terminal message should contain ANSI escape codes
	if !strings.Contains(terminalMsg, "\x1b[") {
		t.Error("Terminal context error should contain ANSI codes, but didn't")
		t.Logf("Terminal error: %s", terminalMsg)
	}

	// Plain message should NOT contain ANSI escape codes
	if strings.Contains(plainMsg, "\x1b[") {
		t.Error("Plain context error should not contain ANSI codes, but did")
		t.Logf("Plain error: %s", plainMsg)
	}

	// Plain message should contain position info
	if !strings.Contains(plainMsg, "position") {
		t.Error("Plain error should contain position information")
		t.Logf("Plain error: %s", plainMsg)
	}

	t.Logf("✓ Terminal error (%d chars): %s", len(terminalMsg), terminalMsg[:min(100, len(terminalMsg))])
	t.Logf("✓ Plain error (%d chars): %s", len(plainMsg), plainMsg)
}

// TestErrorContext_BackwardCompatibility verifies that ParseAskCommandWithVerbosity defaults to terminal
func TestErrorContext_BackwardCompatibility(t *testing.T) {
	query := []string{"ALICE", "is", "engineer", "since", "invalid_date"}

	// Old function should default to Terminal context for backward compatibility
	_, err := ParseAskCommandWithVerbosity(query, 0)
	if err == nil {
		t.Fatal("Expected error for invalid temporal expression")
	}

	// Should contain ANSI codes (terminal context)
	if !strings.Contains(err.Error(), "\x1b[") {
		t.Error("ParseAskCommandWithVerbosity should default to Terminal context (with ANSI codes)")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
