package wslogs

import (
	"sync"

	"go.uber.org/zap/zapcore"
)

// WebSocketCore is a custom zap core that sends logs to WebSocket clients
// It implements the zapcore.Core interface and can be added to a zap logger
// using zapcore.NewTee() to enable multi-output logging
type WebSocketCore struct {
	zapcore.LevelEnabler
	batcher *Batcher
	mu      sync.RWMutex
}

// NewWebSocketCore creates a new WebSocket logging core
// level determines which log levels are sent to WebSocket clients
func NewWebSocketCore(level zapcore.LevelEnabler) *WebSocketCore {
	return &WebSocketCore{
		LevelEnabler: level,
	}
}

// SetBatcher sets the current batcher for collecting logs
// This should be called at the start of query processing
func (c *WebSocketCore) SetBatcher(batcher *Batcher) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batcher = batcher
}

// ClearBatcher removes the current batcher
// This should be called after query processing completes
func (c *WebSocketCore) ClearBatcher() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batcher = nil
}

// With adds structured context to the logger (zap interface)
// For our WebSocket core, we don't need to clone since we're stateless
func (c *WebSocketCore) With(fields []zapcore.Field) zapcore.Core {
	// Return self since we don't maintain field state
	return c
}

// Check determines if the logger should log at this level (zap interface)
func (c *WebSocketCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}

// Write writes a log entry to the WebSocket (zap interface)
// This is where the magic happens - logs are captured and added to the current batch
func (c *WebSocketCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// Check level first (defensive check even though Check should filter)
	// This ensures correct behavior when Write is called directly (e.g., in tests)
	if !c.Enabled(entry.Level) {
		return nil
	}

	c.mu.RLock()
	batcher := c.batcher
	c.mu.RUnlock()

	// If no batcher is set (not in a query context), skip WebSocket output
	// This prevents logs from being sent when no query is being processed
	if batcher == nil {
		return nil
	}

	// Convert zap entry to our message format
	msg := FromZapEntry(entry, fields)

	// Append to current batch
	batcher.Append(msg)

	return nil
}

// Sync flushes any buffered log entries (zap interface)
// For WebSocket core, this is a no-op since batching is explicit
func (c *WebSocketCore) Sync() error {
	return nil
}
