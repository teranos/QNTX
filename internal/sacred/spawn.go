package sacred

// A panic in a goroutine is the one error the axiom cannot reach.
//
// "ERRORS ARE SACRED, WE NEVER DROP, SUPRESS, TRUNCATE, OR ADD LIES TO THEM"
//
// Go gives whoever writes `go func()` no way to hear that goroutine panic. The
// runtime prints a trace to stderr and ends the process, so nothing wraps it,
// nothing logs it, and nothing reaches Sentry. The node dies having said the
// one thing nobody was listening to.
//
// That is not hypothetical. A watcher upsert arriving on a WebSocket
// dereferenced a nil and took the whole node down with exit 2. systemd
// restarted it, every session died with the process because sessions are held
// in memory, and the owner could not log in for three days. Sentry recorded
// 122 plugin health warnings over those days and not one word about the crash.
//
// Go is what this package answers: every goroutine QNTX starts goes through
// here, so a panic becomes a logged error with its stack, and the node keeps
// serving everybody who was not in that goroutine.

import (
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/teranos/QNTX/internal/logger"
)

// Go runs fn in a goroutine that cannot die silently.
//
// name is what the goroutine is for, and it is required rather than derived:
// a stack alone says which function panicked, never which of the sixty places
// that start one was doing it or why.
func Go(name string, fn func()) {
	go func() {
		defer Said(name)
		fn()
	}()
}

// GoTracked is Go for a goroutine the caller waits on. The counter is marked
// done however the goroutine ends, so a panic cannot hang a shutdown that is
// waiting for a goroutine the runtime already took away.
func GoTracked(wg *sync.WaitGroup, name string, fn func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer Said(name)
		fn()
	}()
}

// Said recovers a panic and reports it. Deferred first thing in a goroutine
// this package did not start, for the cases Go cannot reach.
//
// It does not re-panic. One caller's bad message is not grounds to end every
// other session on the node, and a process that dies to prove a point takes
// the report down with it.
func Said(name string) {
	blew := recover()
	if blew == nil {
		return
	}
	logger.Errorw("A goroutine panicked; the node kept serving and this is what it said",
		"goroutine", name,
		"panic", fmt.Sprintf("%v", blew),
		"stack", string(debug.Stack()),
	)
}
