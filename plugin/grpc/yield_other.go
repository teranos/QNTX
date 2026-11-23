//go:build !linux

package grpc

import (
	errors "github.com/teranos/sacred-error"
	"golang.org/x/sys/unix"
)

// giveWay sets the process pid to the lowest priority the system has for it.
// Only Linux has an idle class for the CPU and the disk.
func giveWay(pid int) error {
	if err := unix.Setpriority(unix.PRIO_PROCESS, pid, 19); err != nil {
		return errors.Wrapf(err, "process %d is not at the lowest priority", pid)
	}
	return nil
}

// MemoryPressure: only Linux says how pressed its memory is.
func MemoryPressure() (avg10, avg60 float64, said bool) {
	return 0, 0, false
}
