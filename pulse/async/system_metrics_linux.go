//go:build linux

package async

import (
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/teranos/QNTX/errors"
)

// getMemoryStats returns current memory usage in bytes (Linux only)
func getMemoryStats() (total uint64, available uint64, err error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to get memory stats")
	}

	return v.Total, v.Available, nil
}
