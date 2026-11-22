//go:build linux

package grpc

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	errors "github.com/teranos/sacred-error"
	"golang.org/x/sys/unix"
)

// ioprioIdle is ioprio_set(2)'s value for the idle class: a disk request of
// this process is served when no other is waiting.
const (
	ioprioWhoProcess = 1
	ioprioClassIdle  = 3
	ioprioClassShift = 13
)

// giveWay puts every thread of the process pid at SCHED_IDLE, which runs only
// on a CPU nothing else wants, and in the idle class for the disk.
func giveWay(pid int) error {
	tasks, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "task"))
	if err != nil {
		return errors.Wrapf(err, "the threads of process %d did not read", pid)
	}
	var failed error
	for _, task := range tasks {
		tid, err := strconv.Atoi(task.Name())
		if err != nil {
			continue
		}
		attr := unix.SchedAttr{Size: unix.SizeofSchedAttr, Policy: unix.SCHED_IDLE}
		if err := unix.SchedSetAttr(tid, &attr, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			failed = alongside(failed, errors.Wrapf(err, "thread %d of process %d is not at SCHED_IDLE", tid, pid))
		}
		if _, _, errno := unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), ioprioClassIdle<<ioprioClassShift); errno != 0 && errno != unix.ESRCH {
			failed = alongside(failed, errors.Wrapf(errno, "thread %d of process %d is not in the idle class for the disk", tid, pid))
		}
	}
	return failed
}

// MemoryPressure is the share of the last ten and sixty seconds in which some
// task waited for memory, in percent, as the kernel says it
// (/proc/pressure/memory); and whether it says it.
func MemoryPressure() (avg10, avg60 float64, said bool) {
	f, err := os.Open("/proc/pressure/memory")
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = f.Close() }()
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) == 0 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			key, value, _ := strings.Cut(field, "=")
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
			switch key {
			case "avg10":
				avg10 = v
			case "avg60":
				avg60 = v
			}
		}
		return avg10, avg60, true
	}
	return 0, 0, false
}
