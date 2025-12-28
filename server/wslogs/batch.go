package wslogs

import (
	"sync"
	"time"
)

// Batcher collects log messages for batch sending
// This allows all logs from a single query to be sent together
type Batcher struct {
	messages  []Message
	queryID   string
	transport *Transport
	mu        sync.Mutex
}

// NewBatcher creates a new log batcher for a query
func NewBatcher(queryID string, transport *Transport) *Batcher {
	return &Batcher{
		messages:  make([]Message, 0, 32), // Pre-allocate for typical query
		queryID:   queryID,
		transport: transport,
	}
}

// Append adds a log message to the batch
func (b *Batcher) Append(msg Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, msg)
}

// Flush sends all collected messages as a batch and clears the buffer
func (b *Batcher) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.messages) == 0 {
		return
	}

	batch := &Batch{
		Messages:  b.messages,
		QueryID:   b.queryID,
		Timestamp: time.Now(),
	}

	b.transport.SendBatch(batch)

	// Reuse slice capacity by resetting length to 0
	b.messages = b.messages[:0]
}

// Count returns the number of messages currently in the batch
func (b *Batcher) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.messages)
}
