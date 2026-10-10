//go:build linux

package claudecode

import (
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
	"golang.org/x/sys/unix"
)

// exited says once the process pid has ended, read off a pidfd: the node need
// not be its parent, as a node that took a turn up is not.
func exited(pid int) <-chan error {
	ended := make(chan error, 1)
	fd, err := unix.PidfdOpen(pid, 0)
	if errors.Is(err, unix.ESRCH) {
		ended <- nil
		return ended
	}
	if err != nil {
		ended <- errors.Wrapf(err, "process %d cannot be watched", pid)
		return ended
	}
	sacred.Go("claudecode.exited", func() {
		ended <- sqlclose.With(pollOn(fd, pid), unix.Close(fd), "the pidfd watching the turn's process")
	})
	return ended
}

// pollOn waits for the process fd is the pidfd of to end.
func pollOn(fd, pid int) error {
	polled := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		ready, pollErr := unix.Poll(polled, -1)
		if errors.Is(pollErr, unix.EINTR) || (pollErr == nil && ready < 1) {
			continue
		}
		return errors.Wrapf(pollErr, "process %d stopped being watched", pid)
	}
}
