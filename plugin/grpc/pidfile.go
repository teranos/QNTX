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
			proc.Kill()
		}
		p.logger.Infow("Killed stale plugin process", "pid", pid)
	}

	os.Remove(p.path)
}

// Add appends a PID to the file.
func (p *pidFile) Add(pid int) {
	f, err := os.OpenFile(p.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		p.logger.Warnw("Failed to write plugin PID file", "path", p.path, "error", err)
		return
	}
	defer f.Close()
	f.WriteString(strconv.Itoa(pid) + "\n")
}

// Remove deletes the PID file (called on clean shutdown).
func (p *pidFile) Remove() {
	os.Remove(p.path)
}
