package parity

import "testing"

// TestEnginesArePinned: each storage engine has exactly one pin here, and the
// pin names its version and commit.
func TestEnginesArePinned(t *testing.T) {
	for _, engine := range []string{"sqlite", "duckdb", "postgres"} {
		pin, err := PinOf(".", engine)
		if err != nil {
			t.Fatalf("PinOf(%q): %v", engine, err)
		}
		if pin.Version == "" {
			t.Errorf("PinOf(%q) names no version", engine)
		}
		if pin.Rev == "" {
			t.Errorf("PinOf(%q) names no commit", engine)
		}
	}
}

func TestAnEngineNotPinnedIsAnError(t *testing.T) {
	if got, err := PinOf(".", "mysql"); err == nil {
		t.Errorf("PinOf(%q) = %v, want an error", "mysql", got)
	}
}
