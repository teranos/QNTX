package ax

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestMatcherLogging verifies that the CGO matcher logs operations when a logger is set
func TestMatcherLogging(t *testing.T) {
	// Skip if not built with rustfuzzy tag
	matcher, err := NewCGOMatcher()
	if err != nil {
		t.Skip("CGO matcher not available (build with -tags rustfuzzy)")
	}
	defer matcher.Close()

	// Create observable logger
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()

	// Set logger on matcher
	matcher.SetLogger(logger)

	// Perform a fuzzy match
	predicates := []string{"works_at", "occupation", "employment_status", "works_for"}
	matches := matcher.FindMatches("works", predicates)

	// Verify matches were found
	if len(matches) == 0 {
		t.Error("Expected matches for 'works', got none")
	}

	// Verify debug logs were emitted
	entries := logs.All()
	foundMatchLog := false
	foundIndexLog := false

	for _, entry := range entries {
		if entry.Message == "rust fuzzy match" {
			foundMatchLog = true
			// Verify expected fields
			fields := entry.ContextMap()
			if _, ok := fields["query"]; !ok {
				t.Error("Expected 'query' field in match log")
			}
			if _, ok := fields["matches"]; !ok {
				t.Error("Expected 'matches' field in match log")
			}
			if _, ok := fields["strategy"]; !ok {
				t.Error("Expected 'strategy' field in match log")
			}
			if _, ok := fields["time_us"]; !ok {
				t.Error("Expected 'time_us' field in match log")
			}
		}
		if entry.Message == "rebuilt rust fuzzy predicate index" {
			foundIndexLog = true
		}
	}

	if !foundMatchLog {
		t.Error("Expected debug log for rust fuzzy match")
	}
	if !foundIndexLog {
		t.Error("Expected debug log for index rebuild")
	}
}

// TestMatcherLoggingErrors verifies that errors are logged
func TestMatcherLoggingErrors(t *testing.T) {
	matcher, err := NewCGOMatcher()
	if err != nil {
		t.Skip("CGO matcher not available (build with -tags rustfuzzy)")
	}
	defer matcher.Close()

	// Create observable logger
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()
	matcher.SetLogger(logger)

	// Close the matcher to force an error
	matcher.Close()

	// Try to use it (should log error)
	matches := matcher.FindMatches("test", []string{"test"})

	if len(matches) != 0 {
		t.Error("Expected no matches from closed matcher")
	}

	// Verify error was logged
	entries := logs.FilterMessage("rust fuzzy match failed").All()
	if len(entries) == 0 {
		t.Error("Expected error log for failed match")
	}
}

// TestMatcherWithoutLogger verifies matcher works without a logger
func TestMatcherWithoutLogger(t *testing.T) {
	matcher, err := NewCGOMatcher()
	if err != nil {
		t.Skip("CGO matcher not available (build with -tags rustfuzzy)")
	}
	defer matcher.Close()

	// Don't set logger - should still work
	predicates := []string{"works_at", "occupation"}
	matches := matcher.FindMatches("works", predicates)

	if len(matches) == 0 {
		t.Error("Expected matches even without logger")
	}
}
