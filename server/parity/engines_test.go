package parity

import "testing"

// TestEnginesArePinned reads the pins this directory holds.
func TestEnginesArePinned(t *testing.T) {
	for engine, want := range map[string]string{"sqlite": "3.46.0", "duckdb": "1.4.3"} {
		got, err := Pinned(".", engine)
		if err != nil {
			t.Fatalf("Pinned(%q): %v", engine, err)
		}
		if got != want {
			t.Errorf("Pinned(%q) = %q, want %q", engine, got, want)
		}
	}
}

func TestAnEngineNotPinnedIsAnError(t *testing.T) {
	if got, err := Pinned(".", "postgres"); err == nil {
		t.Errorf("Pinned(%q) = %q, want an error", "postgres", got)
	}
}
