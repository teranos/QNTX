package wslogs

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/logger"
	"go.uber.org/zap/zapcore"
)

// TestVerbosityToLevel tests the verbosity to zap level mapping
func TestVerbosityToLevel(t *testing.T) {
	tests := []struct {
		verbosity int
		expected  zapcore.Level
	}{
		{0, zapcore.WarnLevel},
		{1, zapcore.InfoLevel},
		{2, zapcore.DebugLevel},
		{3, zapcore.DebugLevel},
		{4, zapcore.DebugLevel},
		{10, zapcore.DebugLevel},
	}

	for _, tt := range tests {
		t.Run(string(rune('0'+tt.verbosity)), func(t *testing.T) {
			level := logger.VerbosityToLevel(tt.verbosity)
			if level != tt.expected {
				t.Errorf("logger.VerbosityToLevel(%d) = %v, want %v", tt.verbosity, level, tt.expected)
			}
		})
	}
}

// TestShouldLogTrace tests the trace logging threshold
func TestShouldLogTrace(t *testing.T) {
	tests := []struct {
		verbosity int
		expected  bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},
		{4, true},
		{10, true},
	}

	for _, tt := range tests {
		result := logger.ShouldLogTrace(tt.verbosity)
		if result != tt.expected {
			t.Errorf("logger.ShouldLogTrace(%d) = %v, want %v", tt.verbosity, result, tt.expected)
		}
	}
}

// TestShouldLogAll tests the all logging threshold
func TestShouldLogAll(t *testing.T) {
	tests := []struct {
		verbosity int
		expected  bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, false},
		{4, true},
		{10, true},
	}

	for _, tt := range tests {
		result := logger.ShouldLogAll(tt.verbosity)
		if result != tt.expected {
			t.Errorf("logger.ShouldLogAll(%d) = %v, want %v", tt.verbosity, result, tt.expected)
		}
	}
}

// TestLevelName tests the human-readable level name function
func TestLevelName(t *testing.T) {
	tests := []struct {
		verbosity int
		expected  string
	}{
		{0, "User"},
		{1, "Info (-v)"},
		{2, "Debug (-vv)"},
		{3, "Trace (-vvv)"},
		{4, "All (-vvvv)"},
		{5, "All (-vvvv+)"},
	}

	for _, tt := range tests {
		result := logger.LevelName(tt.verbosity)
		if result != tt.expected {
			t.Errorf("logger.LevelName(%d) = %q, want %q", tt.verbosity, result, tt.expected)
		}
	}
}

// TestBatcher tests the log batching functionality
func TestBatcher(t *testing.T) {
	transport := NewTransport()
	queryID := "test_query_123"

	// Create a channel to receive batches
	receiveChan := make(chan *Batch, 1)
	transport.RegisterClient("test_client", receiveChan)

	// Create batcher
	batcher := NewBatcher(queryID, transport)

	// Verify initial state
	if batcher.Count() != 0 {
		t.Errorf("New batcher should have 0 messages, got %d", batcher.Count())
	}

	// Append messages
	msg1 := Message{
		Level:     "INFO",
		Timestamp: time.Now(),
		Logger:    "test",
		Message:   "First message",
	}
	msg2 := Message{
		Level:     "DEBUG",
		Timestamp: time.Now(),
		Logger:    "test",
		Message:   "Second message",
	}

	batcher.Append(msg1)
	batcher.Append(msg2)

	if batcher.Count() != 2 {
		t.Errorf("Batcher should have 2 messages, got %d", batcher.Count())
	}

	// Flush and verify
	batcher.Flush()

	select {
	case batch := <-receiveChan:
		if batch.QueryID != queryID {
			t.Errorf("Batch QueryID = %q, want %q", batch.QueryID, queryID)
		}
		if len(batch.Messages) != 2 {
			t.Errorf("Batch has %d messages, want 2", len(batch.Messages))
		}
		if batch.Messages[0].Message != "First message" {
			t.Errorf("First message = %q, want %q", batch.Messages[0].Message, "First message")
		}
		if batch.Messages[1].Message != "Second message" {
			t.Errorf("Second message = %q, want %q", batch.Messages[1].Message, "Second message")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for batch")
	}

	// Verify buffer cleared after flush
	if batcher.Count() != 0 {
		t.Errorf("Batcher should be empty after flush, got %d messages", batcher.Count())
	}
}

// TestTransport tests the transport registration and delivery
func TestTransport(t *testing.T) {
	transport := NewTransport()

	// Test client registration
	ch1 := make(chan *Batch, 1)
	ch2 := make(chan *Batch, 1)

	transport.RegisterClient("client1", ch1)
	transport.RegisterClient("client2", ch2)

	if transport.ClientCount() != 2 {
		t.Errorf("Transport should have 2 clients, got %d", transport.ClientCount())
	}

	// Test batch delivery to all clients
	batch := &Batch{
		QueryID: "test_query",
		Messages: []Message{
			{
				Level:     "INFO",
				Timestamp: time.Now(),
				Logger:    "test",
				Message:   "Test message",
			},
		},
		Timestamp: time.Now(),
	}

	transport.SendBatch(batch)

	// Verify both clients received the batch
	select {
	case <-ch1:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("Client 1 did not receive batch")
	}

	select {
	case <-ch2:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("Client 2 did not receive batch")
	}

	// Test client unregistration
	transport.UnregisterClient("client1")

	if transport.ClientCount() != 1 {
		t.Errorf("Transport should have 1 client after unregister, got %d", transport.ClientCount())
	}
}

// TestWebSocketCore tests the WebSocket core functionality
func TestWebSocketCore(t *testing.T) {
	transport := NewTransport()
	core := NewWebSocketCore(zapcore.InfoLevel)

	// Without a batcher, Write should not send anything
	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Time:    time.Now(),
		Message: "Test message",
	}

	err := core.Write(entry, nil)
	if err != nil {
		t.Errorf("Write without batcher should not error, got %v", err)
	}

	// With a batcher, logs should be collected
	receiveChan := make(chan *Batch, 1)
	transport.RegisterClient("test", receiveChan)

	batcher := NewBatcher("query1", transport)
	core.SetBatcher(batcher)

	err = core.Write(entry, nil)
	if err != nil {
		t.Errorf("Write with batcher should not error, got %v", err)
	}

	if batcher.Count() != 1 {
		t.Errorf("Batcher should have 1 message, got %d", batcher.Count())
	}

	// Clear batcher
	core.ClearBatcher()

	// Write after clear should not add to the old batcher
	err = core.Write(entry, nil)
	if err != nil {
		t.Errorf("Write after clear should not error, got %v", err)
	}

	if batcher.Count() != 1 {
		t.Errorf("Batcher count should still be 1, got %d", batcher.Count())
	}
}

// TestWebSocketCoreVerbosityFiltering tests that the core respects log levels
func TestWebSocketCoreVerbosityFiltering(t *testing.T) {
	transport := NewTransport()

	// Create core at Info level
	core := NewWebSocketCore(zapcore.InfoLevel)

	receiveChan := make(chan *Batch, 1)
	transport.RegisterClient("test", receiveChan)

	batcher := NewBatcher("query1", transport)
	core.SetBatcher(batcher)

	// Debug log should be filtered out
	debugEntry := zapcore.Entry{
		Level:   zapcore.DebugLevel,
		Time:    time.Now(),
		Message: "Debug message",
	}

	err := core.Write(debugEntry, nil)
	if err != nil {
		t.Errorf("Write should not error, got %v", err)
	}

	// Batcher should still be empty (debug filtered)
	if batcher.Count() != 0 {
		t.Errorf("Batcher should be empty (debug filtered), got %d", batcher.Count())
	}

	// Info log should pass through
	infoEntry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Time:    time.Now(),
		Message: "Info message",
	}

	err = core.Write(infoEntry, nil)
	if err != nil {
		t.Errorf("Write should not error, got %v", err)
	}

	if batcher.Count() != 1 {
		t.Errorf("Batcher should have 1 message, got %d", batcher.Count())
	}
}
