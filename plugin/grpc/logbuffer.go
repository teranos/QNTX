package grpc

import (
	"sync"
	"time"
)

// LogEntry represents a single log line from a plugin process.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Line      string    `json:"line"`
	Source    string    `json:"source"` // "stdout" or "stderr"
}

// LogBuffer is a fixed-size ring buffer that captures recent plugin log entries
// and supports pub/sub for live streaming.
type LogBuffer struct {
	mu          sync.RWMutex
	entries     []LogEntry
	capacity    int
	head        int // next write position
	count       int // entries currently stored
	subscribers map[chan LogEntry]struct{}
}

// NewLogBuffer creates a ring buffer with the given capacity.
func NewLogBuffer(capacity int) *LogBuffer {
	return &LogBuffer{
		entries:     make([]LogEntry, capacity),
		capacity:    capacity,
		subscribers: make(map[chan LogEntry]struct{}),
	}
}

// Write appends an entry to the ring and fans out to all subscribers.
func (b *LogBuffer) Write(entry LogEntry) {
	b.mu.Lock()
	b.entries[b.head] = entry
	b.head = (b.head + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}

	// Snapshot subscribers under lock to avoid holding lock during send
	subs := make([]chan LogEntry, 0, len(b.subscribers))
	for ch := range b.subscribers {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	// Non-blocking fan-out — drop if subscriber is slow
	for _, ch := range subs {
		select {
		case ch <- entry:
		default:
		}
	}
}

// Recent returns the last n entries in chronological order.
func (b *LogBuffer) Recent(n int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n > b.count {
		n = b.count
	}
	if n == 0 {
		return nil
	}

	result := make([]LogEntry, n)
	// Start position: oldest of the n entries we want
	start := (b.head - n + b.capacity) % b.capacity
	for i := 0; i < n; i++ {
		result[i] = b.entries[(start+i)%b.capacity]
	}
	return result
}

// Subscribe returns a channel that receives new log entries.
// The channel is buffered to absorb short bursts without blocking writers.
func (b *LogBuffer) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 64)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (b *LogBuffer) Unsubscribe(ch chan LogEntry) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}
