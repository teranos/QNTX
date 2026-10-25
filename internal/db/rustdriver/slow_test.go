//go:build !quickdev

package rustdriver

import (
	"testing"
	"time"
)

// A call that waits for its connection, or runs, past the bound names its
// statement; one under it says nothing.
func TestASlowCallIsToldWithItsStatement(t *testing.T) {
	var told []SlowCall
	OnSlowCall(time.Second, func(c SlowCall) { told = append(told, c) })
	t.Cleanup(func() { watching.Store(nil) })

	at := time.Date(2026, 10, 10, 5, 19, 0, 0, time.UTC)
	timed("read", "", "SELECT 1", at, at.Add(10*time.Millisecond), at.Add(20*time.Millisecond))
	timed("read", "ops", "SELECT * FROM async_ix_jobs", at, at.Add(3*time.Second), at.Add(3*time.Second+time.Millisecond))
	timed("write", "ops", "a transaction, BEGIN to COMMIT", at, at, at.Add(4*time.Second))

	if len(told) != 2 {
		t.Fatalf("told %d calls, want the 2 past a second: %+v", len(told), told)
	}
	if told[0].SQL != "SELECT * FROM async_ix_jobs" || told[0].Waited != 3*time.Second || told[0].Caller != "ops" {
		t.Errorf("the call that waited is told as %+v", told[0])
	}
	if told[1].Conn != "write" || told[1].Ran != 4*time.Second {
		t.Errorf("the transaction that held is told as %+v", told[1])
	}
}
