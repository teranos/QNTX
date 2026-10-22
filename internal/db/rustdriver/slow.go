//go:build !quickdev

package rustdriver

import (
	"sync/atomic"
	"time"
)

// The pool says how many connections are in use, never which statement holds
// the store they all go through. A call into Rust is timed where it is made.

// SlowCall is one call into Rust that waited for its connection's mutex, or ran,
// for longer than the bound it was watched with.
type SlowCall struct {
	// Conn is the connection the call went to: read or write.
	Conn string
	// Caller is the flight recorder tag of whoever opened the database.
	Caller string
	// SQL is the statement with its placeholders, never its values.
	SQL string
	// Waited is the time spent for the connection's mutex before the call.
	Waited time.Duration
	// Ran is the time spent inside Rust, or a transaction's whole hold.
	Ran time.Duration
}

type slowWatch struct {
	after time.Duration
	tell  func(SlowCall)
}

var watching atomic.Pointer[slowWatch]

// OnSlowCall tells tell of every call that waits or runs past after, from now
// on. Until it is called nothing is told.
func OnSlowCall(after time.Duration, tell func(SlowCall)) {
	watching.Store(&slowWatch{after: after, tell: tell})
}

// timed tells of a call that was asked at asked, held its mutex from held, and
// was done at done.
func timed(conn, caller, sql string, asked, held, done time.Time) {
	w := watching.Load()
	if w == nil {
		return
	}
	waited, ran := held.Sub(asked), done.Sub(held)
	if waited < w.after && ran < w.after {
		return
	}
	w.tell(SlowCall{Conn: conn, Caller: caller, SQL: sql, Waited: waited, Ran: ran})
}
