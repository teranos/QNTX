//go:build windows

package async

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/mem"
)

// getMemoryStats returns current memory usage in bytes (Windows only)
func getMemoryStats() (total uint64, available uint64, err error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get memory stats: %w", err)
	}

	return v.Total, v.Available, nil
}
