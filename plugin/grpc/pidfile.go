package grpc

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"go.uber.org/zap"
)

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

// CleanStale reads the PID file from a previous run and kills any surviving processes.
// Removes the file afterward regardless of outcome.
func (p *pidFile) CleanStale() {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return // no file = nothing to clean
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
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
		p.logger.Warnw("Failed to remove stale PID file; next startup will re-clean it",
			"path", p.path, "error", err)
	}
}

// Add appends a PID to the file. An unrecorded PID is an orphan on the next
// restart — CleanStale finds nothing to kill and the old plugin keeps its
// ports and DB locks — so every failure here is said, not swallowed.
func (p *pidFile) Add(pid int) {
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		p.logger.Warnw("Failed to open plugin PID file; this plugin will be orphaned if QNTX restarts uncleanly",
			"path", p.path, "pid", pid, "error", err)
		return
	}
	if _, err := f.WriteString(strconv.Itoa(pid) + "\n"); err != nil {
		p.logger.Warnw("Failed to record plugin PID; this plugin will be orphaned if QNTX restarts uncleanly",
			"path", p.path, "pid", pid, "error", err)
	}
	if err := f.Close(); err != nil {
		p.logger.Warnw("Failed to close plugin PID file; the PID may not have reached disk",
			"path", p.path, "pid", pid, "error", err)
	}
}

// Remove deletes the PID file (called on clean shutdown).
func (p *pidFile) Remove() {
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		p.logger.Warnw("Failed to remove plugin PID file on shutdown; next startup will kill PIDs it lists",
			"path", p.path, "error", err)
	}
}
