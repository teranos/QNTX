//go:build cgo

package sqlitecgo

import (
	"time"

	"github.com/teranos/QNTX/internal/logger"
)

// writeRequest is a unit of work submitted to the write queue.
type writeRequest struct {
	fn     func() error
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
	go rs.writeLoop()
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
	rs.muWrite.Lock()
	err := req.fn()
	rs.muWrite.Unlock()
	req.result <- err
}

// SubmitWrite submits a write to the appropriate priority queue and blocks
// until it completes. Returns the error from the write function.
func (rs *RustStore) SubmitWrite(high bool, fn func() error) error {
	req := writeRequest{fn: fn, result: make(chan error, 1)}

	ch := rs.lowPriority
	if high {
		ch = rs.highPriority
	}

	if ch == nil {
		// Queue not started — fall back to direct execution
		rs.muWrite.Lock()
		err := fn()
		rs.muWrite.Unlock()
		return err
	}

	waitStart := time.Now()
	ch <- req
	err := <-req.result
	elapsed := time.Since(waitStart)

	if high && elapsed >= slowOpThreshold {
		// High-priority waits are always worth logging individually.
		logger.Logger.Warnw("High-priority write wait", "duration", elapsed)
	} else {
		// Low-priority waits are expected — aggregate via recordWait.
		recordWait(waitStart, "write_queue")
	}
	return err
}
