//go:build windows

package async

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/teranos/QNTX/errors"
)

// getMemoryStats returns current memory usage in bytes (Windows only)
func getMemoryStats() (total uint64, available uint64, err error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to get memory stats")
	}

	return v.Total, v.Available, nil
}

// getCPUPercent returns aggregate CPU utilization (0-100) over a 1s sample window.
func getCPUPercent() (float64, error) {
	pcts, err := cpu.Percent(1*time.Second, false)
	if err != nil {
		return 0, errors.Wrap(err, "failed to get CPU stats")
	}
	if len(pcts) == 0 {
		return 0, errors.New("cpu.Percent returned empty slice")
	}
	return pcts[0], nil
}
