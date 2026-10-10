package grpc

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// pids is where a manager records the processes it launches, so the next run
// can kill what this one left behind.
type pids interface {
	// CleanStale kills what a previous run left and forgets it.
	CleanStale() error
	// Add records a launched process.
	Add(pid int) error
	// Remove forgets every process, on a clean shutdown.
	Remove() error
}

// unrecordedPids is a manager not handed a PID file: what it launches is not
// recorded, so a run that ends uncleanly leaves its plugins for nothing to kill.
type unrecordedPids struct{}

func (unrecordedPids) CleanStale() error { return nil }
func (unrecordedPids) Add(int) error     { return nil }
func (unrecordedPids) Remove() error     { return nil }

// pidFile tracks plugin process IDs for cleanup across restarts.
// Each QNTX instance writes its plugin PIDs to a file keyed by server port,
// so multiple instances on the same machine don't interfere.
type pidFile struct {
	path   string
	logger *zap.SugaredLogger
}

func newPidFile(path string, logger *zap.SugaredLogger) *pidFile {
	return &pidFile{
		path:   path,
		logger: logger,
	}
}

// CleanStale reads the PID file from a previous run, kills any surviving
// processes, and removes the file. No file is a previous run that left nothing.
// A file that stays is refused: the next run would kill the PIDs it lists,
// which by then may be anyone's.
func (p *pidFile) CleanStale() error {
	data, err := os.ReadFile(p.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "failed to read the plugin PID file %s", p.path)
	}

	for _, line := range strings.Split(string(data), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		// Don't kill ourselves
		if pid == os.Getpid() {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		// Check if process is still alive (signal 0 tests existence)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			continue // already dead
		}
		// Kill entire process group to also terminate children (e.g. Reticulum)
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			// Fallback to single-process kill if group kill fails
			if killErr := proc.Kill(); killErr != nil {
				p.logger.Warnw("Stale plugin process survived cleanup; it may hold ports or DB locks",
					"pid", pid, "group_kill_error", err, "kill_error", killErr)
				continue
			}
		}
		p.logger.Infow("Killed stale plugin process", "pid", pid)
	}

	if err := os.Remove(p.path); err != nil {
		return errors.Wrapf(err, "failed to remove the stale plugin PID file %s", p.path)
	}
	return nil
}

// Add appends a PID to the file. An unrecorded PID is an orphan on the next
// restart — CleanStale finds nothing to kill and the old plugin keeps its
// ports and DB locks — so every failure here is returned, not swallowed.
func (p *pidFile) Add(pid int) (err error) {
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrapf(err, "failed to open the plugin PID file %s to record pid %d", p.path, pid)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = alongside(err, errors.Wrapf(closeErr, "failed to close the plugin PID file %s after recording pid %d", p.path, pid))
		}
	}()
	line := strconv.Itoa(pid) + "\n"
	if written, err := f.WriteString(line); err != nil {
		return errors.Wrapf(err, "failed to record pid %d in %s: %d of %d bytes written", pid, p.path, written, len(line))
	}
	return nil
}

// Remove deletes the PID file (called on clean shutdown). A file that stays
// has the next startup kill the PIDs it lists.
func (p *pidFile) Remove() error {
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to remove the plugin PID file %s; the next startup kills the PIDs it lists", p.path)
	}
	return nil
}
