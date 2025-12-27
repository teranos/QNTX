//go:build linux

package async

import (
	"testing"
)

// ============================================================================
// System Monitor Test Universe (Linux)
// ============================================================================
//
// Theme: System monitoring verifies that memory stats can be read from the OS
// ============================================================================

func TestGetMemoryStats_Linux(t *testing.T) {
	t.Run("system monitor reads memory stats", func(t *testing.T) {
		// System monitor queries OS for current memory usage
		total, available, err := getMemoryStats()

		// Should succeed on Linux systems
		if err != nil {
			t.Fatalf("Failed to get memory stats: %v", err)
		}

		// Verify reasonable values
		if total == 0 {
			t.Error("Expected total memory > 0")
		}

		if available > total {
			t.Errorf("Available memory (%d) cannot exceed total memory (%d)", available, total)
		}

		// Log stats for visibility in CI
		t.Logf("Memory stats: total=%d bytes (%.2f GB), available=%d bytes (%.2f GB)",
			total, float64(total)/(1024*1024*1024),
			available, float64(available)/(1024*1024*1024))
	})
}
