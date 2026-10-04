package parity

import "testing"

// TestEnginesArePinned: each storage engine has exactly one pin here, and the
// pin names its version.
func TestEnginesArePinned(t *testing.T) {
	for _, engine := range []string{"sqlite", "duckdb"} {
		version, err := Pinned(".", engine)
		if err != nil {
			t.Fatalf("Pinned(%q): %v", engine, err)
		}
		if version == "" {
			t.Errorf("Pinned(%q) names no version", engine)
		}
	}
}

func TestAnEngineNotPinnedIsAnError(t *testing.T) {
	if got, err := Pinned(".", "postgres"); err == nil {
		t.Errorf("Pinned(%q) = %q, want an error", "postgres", got)
	}
}
