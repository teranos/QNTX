package logger

import (
	"os"
	"sync"

	errors "github.com/teranos/sacred-error"
)

// fileLogMost is how large the log file grows before it becomes its .1, which
// replaces the one before: the node's file log is at most twice this.
const fileLogMost = 64 << 20

// rotatingFile is the log file, held to fileLogMost.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
	most int64
}

// openRotating opens path for appending; one already past most is moved aside first.
func openRotating(path string, most int64) (*rotatingFile, error) {
	r := &rotatingFile{path: path, most: most}
	if err := r.open(); err != nil {
		return nil, err
	}
	if r.size >= most {
		if err := r.rotate(); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	file, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrapf(err, "failed to open log file %s", r.path)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return errors.Wrapf(err, "failed to read the size of log file %s", r.path)
	}
	r.file, r.size = file, info.Size()
	return nil
}

// rotate moves the file to its .1, replacing the one there, and starts it empty.
func (r *rotatingFile) rotate() error {
	closeErr := r.file.Close()
	r.file = nil
	if closeErr != nil {
		if reopened := r.open(); reopened != nil {
			return errors.WithSecondaryError(errors.Wrapf(closeErr, "failed to close log file %s to rotate it", r.path), reopened)
		}
		return errors.Wrapf(closeErr, "failed to close log file %s to rotate it", r.path)
	}
	if err := os.Rename(r.path, r.path+".1"); err != nil {
		// Not moved aside, it is reopened as it was, and written on.
		if reopened := r.open(); reopened != nil {
			return errors.WithSecondaryError(errors.Wrapf(err, "failed to rotate log file %s", r.path), reopened)
		}
		return errors.Wrapf(err, "failed to rotate log file %s", r.path)
	}
	return r.open()
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.most {
		if err := r.rotate(); err != nil && r.file == nil {
			return 0, err
		}
	}
	if r.file == nil {
		return 0, errors.Newf("log file %s is not open", r.path)
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) WriteString(s string) (int, error) { return r.Write([]byte(s)) }

func (r *rotatingFile) Sync() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	return r.file.Sync()
}
