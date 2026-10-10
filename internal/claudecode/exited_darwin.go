//go:build darwin

package claudecode

import (
	"syscall"

	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
	"golang.org/x/sys/unix"
)

// exited says once the process pid has ended, read off a kqueue: the node need
// not be its parent, as a node that took a turn up is not.
func exited(pid int) <-chan error {
	ended := make(chan error, 1)
	kq, err := unix.Kqueue()
	if err != nil {
		ended <- errors.Wrap(err, "no kqueue to watch the turn's process on")
		return ended
	}
	var watch unix.Kevent_t
	unix.SetKevent(&watch, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	watch.Fflags = unix.NOTE_EXIT
	sacred.Go("claudecode.exited", func() {
		ended <- sqlclose.With(waitOn(kq, watch, pid), unix.Close(kq), "the kqueue watching the turn's process")
	})
	return ended
}

// waitOn sets watch on kq and waits for it to fire.
func waitOn(kq int, watch unix.Kevent_t, pid int) error {
	changes, events := []unix.Kevent_t{watch}, make([]unix.Kevent_t, 1)
	for {
		fired, waitErr := unix.Kevent(kq, changes, events, nil)
		changes = nil
		if errors.Is(waitErr, unix.EINTR) || (waitErr == nil && fired < 1) {
			continue
		}
		if waitErr != nil {
			return errors.Wrapf(waitErr, "process %d stopped being watched", pid)
		}
		// A watch that could not be set comes back as an event saying why: a
		// process already gone is one that ended.
		if events[0].Flags&unix.EV_ERROR == unix.EV_ERROR {
			if watchErr := syscall.Errno(events[0].Data); watchErr != unix.ESRCH {
				return errors.Wrapf(watchErr, "process %d cannot be watched", pid)
			}
		}
		return nil
	}
}
