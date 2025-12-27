//go:build darwin

package async

import (
	"testing"
)

func TestGetMemoryStats(t *testing.T) {
	total, available, err := getMemoryStats()

	if err != nil {
		t.Fatalf("getMemoryStats failed: %v", err)
	}

	t.Logf("Total memory: %d bytes (%.2f GB)", total, float64(total)/1024/1024/1024)
	t.Logf("Available memory: %d bytes (%.2f GB)", available, float64(available)/1024/1024/1024)

	// Sanity checks
	if total == 0 {
		t.Error("Total memory is 0 - detection failed")
	}

	if total < 1024*1024*1024 {
		t.Errorf("Total memory suspiciously low: %.2f GB", float64(total)/1024/1024/1024)
	}

	if total > 1024*1024*1024*1024 {
		t.Errorf("Total memory suspiciously high: %.2f GB", float64(total)/1024/1024/1024)
	}

	if available > total {
		t.Errorf("Available memory (%d) greater than total (%d)", available, total)
	}
}

func TestCalculateSafeWorkerCount(t *testing.T) {
	tests := []struct {
		availableGB float64
		expected    int
	}{
		{1.0, 1},   // Less than buffer
		{3.0, 1},   // 3GB - 2GB buffer = 1GB / 5GB = 0.2, rounds to 1
		{7.0, 1},   // 7GB - 2GB = 5GB / 5GB = 1 worker
		{12.0, 2},  // 12GB - 2GB = 10GB / 5GB = 2 workers
		{27.0, 5},  // 27GB - 2GB = 25GB / 5GB = 5 workers
		{60.0, 10}, // 60GB caps at 10 workers
	}

	for _, tt := range tests {
		result := calculateSafeWorkerCount(tt.availableGB)
		if result != tt.expected {
			t.Errorf("calculateSafeWorkerCount(%.1fGB) = %d, expected %d",
				tt.availableGB, result, tt.expected)
		}
	}
}
