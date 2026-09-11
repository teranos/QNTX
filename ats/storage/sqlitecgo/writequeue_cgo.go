//go:build cgo

package sqlitecgo

import (
	"time"

	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/errors"
)

const writeQueueTimeout = 30 * time.Second

// writeRequest is a unit of work submitted to the write queue.
type writeRequest struct {
	fn     func() error
	caller string // who submitted this write (for watchdog diagnostics)
	result chan error
}

// StartWriteQueue launches a single goroutine that processes writes
// through muWrite. High-priority writes (POST) are always drained
// before low-priority writes (plugins, background).
//
// This eliminates the problem where POST waits behind 20 queued plugin
// writes. POST jumps to the front — it only waits for the single
// currently-executing write to finish.
func (rs *RustStore) StartWriteQueue(highSize, lowSize int) {
	rs.highPriority = make(chan writeRequest, highSize)
	rs.lowPriority = make(chan writeRequest, lowSize)
	// Every write on the node is drained by this one goroutine. If it ends,
	// nothing writes again, so its ending is worth saying out loud.
	sacred.Go("sqlite.writeLoop", rs.writeLoop)
}

func (rs *RustStore) writeLoop() {
	for {
		// Always prefer high-priority writes.
		select {
		case req := <-rs.highPriority:
			rs.executeWrite(req)
			continue
		default:
		}

		// No high-priority work — wait for either.
		select {
		case req := <-rs.highPriority:
			rs.executeWrite(req)
		case req := <-rs.lowPriority:
			rs.executeWrite(req)
		}
	}
}

func (rs *RustStore) executeWrite(req writeRequest) {
	err := rs.runWrite(req)
	rs.ClearWriteHolder()
	req.result <- err
}

// runWrite holds the write lock for exactly one request.
//
// The unlock is deferred and the panic is caught because this is the single
// goroutine every write on the node passes through. A panic in req.fn used to
// end the process; short of that it would have left muWrite held for the
// lifetime of the node and its submitter blocked on a result nobody was left
// to send. One bad write is one failed write, not a node that takes no more.
func (rs *RustStore) runWrite(req writeRequest) (err error) {
	rs.SetWriteHolder(req.caller)
	rs.muWrite.Lock()
	defer rs.muWrite.Unlock()
	defer sacred.Recovered("sqlite.write from "+req.caller, &err)
	return req.fn()
}

// SubmitWrite submits a write to the appropriate priority queue and blocks
// until it completes. Returns the error from the write function.
// The caller label identifies who submitted this write (for watchdog diagnostics).
func (rs *RustStore) SubmitWrite(high bool, caller string, fn func() error) error {
	req := writeRequest{fn: fn, caller: caller, result: make(chan error, 1)}

	ch := rs.lowPriority
	if high {
		ch = rs.highPriority
	}

	if ch == nil {
		// Queue not started — fall back to direct execution
		rs.muWrite.Lock()
		rs.SetWriteHolder(caller)
		err := fn()
		rs.ClearWriteHolder()
		rs.muWrite.Unlock()
		return err
	}

	waitStart := time.Now()

	// Backpressure: if the channel is full, wait up to writeQueueTimeout
	// before giving up. This prevents goroutine pile-up under sustained load.
	select {
	case ch <- req:
	case <-time.After(writeQueueTimeout):
		priority := "low"
		if high {
			priority = "high"
		}
		logger.Logger.Errorw("Write queue full — dropping write",
			"priority", priority,
			"queue_len", len(ch),
			"timeout", writeQueueTimeout,
		)
		return errors.Newf("write queue full: %s-priority write timed out after %s", priority, writeQueueTimeout)
	}

	err := <-req.result
	elapsed := time.Since(waitStart)

	if high && elapsed >= slowOpThreshold {
		logger.Logger.Warnw("High-priority write wait", "duration", elapsed)
	} else {
		recordWait(waitStart, "write_queue")
	}
	return err
}
