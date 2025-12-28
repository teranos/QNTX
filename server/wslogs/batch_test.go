package wslogs

import (
	"sync"
	"testing"
	"time"
)

// TestBatcherBasicOperations tests basic append, flush, and count operations
func TestBatcherBasicOperations(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 1)
	transport.RegisterClient("test_client", receiveChan)

	batcher := NewBatcher("query_123", transport)

	// Initially empty
	if batcher.Count() != 0 {
		t.Errorf("New batcher should have 0 messages, got %d", batcher.Count())
	}

	// Append a message
	msg1 := Message{
		Level:   "INFO",
		Logger:  "test",
		Message: "First message",
	}
	batcher.Append(msg1)

	if batcher.Count() != 1 {
		t.Errorf("Batcher should have 1 message, got %d", batcher.Count())
	}

	// Append another message
	msg2 := Message{
		Level:   "DEBUG",
		Logger:  "test",
		Message: "Second message",
	}
	batcher.Append(msg2)

	if batcher.Count() != 2 {
		t.Errorf("Batcher should have 2 messages, got %d", batcher.Count())
	}

	// Flush
	batcher.Flush()

	// Verify batch was sent
	select {
	case batch := <-receiveChan:
		if batch.QueryID != "query_123" {
			t.Errorf("Batch QueryID = %q, want %q", batch.QueryID, "query_123")
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

// TestBatcherFlushEmpty tests that flushing an empty batcher doesn't send
func TestBatcherFlushEmpty(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 1)
	transport.RegisterClient("test_client", receiveChan)

	batcher := NewBatcher("query_empty", transport)

	// Flush empty batcher
	batcher.Flush()

	// Should not send anything
	select {
	case <-receiveChan:
		t.Error("Should not have received batch from empty batcher")
	case <-time.After(10 * time.Millisecond):
		// Expected - no batch sent
	}
}

// TestBatcherMultipleFlushes tests that batcher can be reused after flush
func TestBatcherMultipleFlushes(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 10)
	transport.RegisterClient("test_client", receiveChan)

	batcher := NewBatcher("query_reuse", transport)

	// First batch
	batcher.Append(Message{Message: "Batch 1 - Message 1"})
	batcher.Append(Message{Message: "Batch 1 - Message 2"})
	batcher.Flush()

	// Second batch (reusing same batcher)
	batcher.Append(Message{Message: "Batch 2 - Message 1"})
	batcher.Flush()

	// Third batch
	batcher.Append(Message{Message: "Batch 3 - Message 1"})
	batcher.Append(Message{Message: "Batch 3 - Message 2"})
	batcher.Append(Message{Message: "Batch 3 - Message 3"})
	batcher.Flush()

	// Verify all three batches were sent correctly
	batches := make([]*Batch, 0, 3)
	for i := 0; i < 3; i++ {
		select {
		case batch := <-receiveChan:
			batches = append(batches, batch)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Timeout waiting for batch %d", i+1)
		}
	}

	if len(batches[0].Messages) != 2 {
		t.Errorf("Batch 1 should have 2 messages, got %d", len(batches[0].Messages))
	}
	if len(batches[1].Messages) != 1 {
		t.Errorf("Batch 2 should have 1 message, got %d", len(batches[1].Messages))
	}
	if len(batches[2].Messages) != 3 {
		t.Errorf("Batch 3 should have 3 messages, got %d", len(batches[2].Messages))
	}
}

// TestBatcherConcurrentAppends tests concurrent appends from multiple goroutines
func TestBatcherConcurrentAppends(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 1)
	transport.RegisterClient("test_client", receiveChan)

	batcher := NewBatcher("query_concurrent", transport)

	var wg sync.WaitGroup
	numGoroutines := 10
	messagesPerGoroutine := 20

	// Concurrently append messages
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				msg := Message{
					Level:   "INFO",
					Logger:  "test",
					Message: "Message from goroutine",
				}
				batcher.Append(msg)
			}
		}(i)
	}

	wg.Wait()

	// Verify all messages were added
	expectedCount := numGoroutines * messagesPerGoroutine
	if batcher.Count() != expectedCount {
		t.Errorf("Batcher should have %d messages, got %d", expectedCount, batcher.Count())
	}

	// Flush and verify
	batcher.Flush()

	select {
	case batch := <-receiveChan:
		if len(batch.Messages) != expectedCount {
			t.Errorf("Batch should have %d messages, got %d", expectedCount, len(batch.Messages))
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for batch")
	}
}

// TestBatcherMessageOrdering tests that messages maintain append order
func TestBatcherMessageOrdering(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 1)
	transport.RegisterClient("test_client", receiveChan)

	batcher := NewBatcher("query_ordering", transport)

	// Append messages with sequential IDs
	numMessages := 100
	for i := 0; i < numMessages; i++ {
		msg := Message{
			Message: string(rune('A' + (i % 26))),
			Fields: map[string]interface{}{
				"sequence": i,
			},
		}
		batcher.Append(msg)
	}

	batcher.Flush()

	select {
	case batch := <-receiveChan:
		if len(batch.Messages) != numMessages {
			t.Fatalf("Expected %d messages, got %d", numMessages, len(batch.Messages))
		}
		// Verify order
		for i := 0; i < numMessages; i++ {
			sequence, ok := batch.Messages[i].Fields["sequence"]
			if !ok {
				t.Errorf("Message %d missing sequence field", i)
				continue
			}
			if sequence != i {
				t.Errorf("Message at index %d has sequence %v, expected %d", i, sequence, i)
			}
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for batch")
	}
}

// TestBatcherConcurrentFlush tests that only one flush happens at a time
func TestBatcherConcurrentFlush(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 10)
	transport.RegisterClient("test_client", receiveChan)

	batcher := NewBatcher("query_flush", transport)

	// Add some messages
	for i := 0; i < 10; i++ {
		batcher.Append(Message{Message: "Test message"})
	}

	var wg sync.WaitGroup
	numFlushers := 5

	// Try to flush concurrently from multiple goroutines
	for i := 0; i < numFlushers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batcher.Flush()
		}()
	}

	wg.Wait()

	// Should receive exactly one batch (or zero if one goroutine flushed before others added anything)
	// The mutex ensures only one flush completes with the messages
	batchCount := 0
	timeout := time.After(50 * time.Millisecond)

loop:
	for {
		select {
		case <-receiveChan:
			batchCount++
		case <-timeout:
			break loop
		}
	}

	// We should get exactly 1 batch because all flushes happened concurrently
	// Only the first one to acquire the lock will send the messages
	// Subsequent flushes will see empty buffer
	if batchCount != 1 {
		t.Errorf("Expected 1 batch from concurrent flushes, got %d", batchCount)
	}

	// Batcher should be empty after flushes
	if batcher.Count() != 0 {
		t.Errorf("Batcher should be empty after flush, got %d messages", batcher.Count())
	}
}

// TestBatcherConcurrentAppendAndFlush tests appending and flushing concurrently
func TestBatcherConcurrentAppendAndFlush(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 100)
	transport.RegisterClient("test_client", receiveChan)

	batcher := NewBatcher("query_mixed", transport)

	var wg sync.WaitGroup
	stopSignal := make(chan bool)
	appendCount := 0
	var countMu sync.Mutex

	// Continuous appending
	numAppenders := 5
	for i := 0; i < numAppenders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopSignal:
					return
				default:
					batcher.Append(Message{Message: "Concurrent message"})
					countMu.Lock()
					appendCount++
					countMu.Unlock()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Periodic flushing
	numFlushers := 2
	for i := 0; i < numFlushers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopSignal:
					return
				default:
					time.Sleep(10 * time.Millisecond)
					batcher.Flush()
				}
			}
		}()
	}

	// Let operations run
	time.Sleep(100 * time.Millisecond)
	close(stopSignal)
	wg.Wait()

	// Final flush to get any remaining messages
	batcher.Flush()

	// Collect all batches
	totalMessages := 0
	timeout := time.After(50 * time.Millisecond)

loop:
	for {
		select {
		case batch := <-receiveChan:
			totalMessages += len(batch.Messages)
		case <-timeout:
			break loop
		}
	}

	// We may not get all messages due to the non-blocking nature of SendBatch
	// But we should get a significant portion
	t.Logf("Appended %d messages, received %d in batches", appendCount, totalMessages)

	// Verify we received at least some messages (not all may arrive due to channel capacity)
	if totalMessages == 0 && appendCount > 0 {
		t.Error("Should have received at least some messages")
	}
}

// TestBatcherConcurrentCount tests that Count is thread-safe
func TestBatcherConcurrentCount(t *testing.T) {
	transport := NewTransport()
	batcher := NewBatcher("query_count", transport)

	var wg sync.WaitGroup
	stopSignal := make(chan bool)

	// Goroutine that appends
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopSignal:
				return
			default:
				batcher.Append(Message{Message: "Test"})
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutines that read count
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopSignal:
					return
				default:
					count := batcher.Count()
					// Just reading - verify it doesn't panic
					_ = count
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Run for a short time
	time.Sleep(50 * time.Millisecond)
	close(stopSignal)
	wg.Wait()

	// Verify final count is reasonable
	finalCount := batcher.Count()
	if finalCount < 0 {
		t.Errorf("Count should not be negative, got %d", finalCount)
	}
}

// TestBatcherCapacityPreallocation tests that pre-allocation works correctly
func TestBatcherCapacityPreallocation(t *testing.T) {
	transport := NewTransport()
	batcher := NewBatcher("query_capacity", transport)

	// The batcher pre-allocates capacity for 32 messages
	// Adding fewer than 32 should not cause reallocation
	for i := 0; i < 32; i++ {
		batcher.Append(Message{Message: "Test message"})
	}

	if batcher.Count() != 32 {
		t.Errorf("Expected 32 messages, got %d", batcher.Count())
	}

	// Adding more should still work
	batcher.Append(Message{Message: "Extra message"})

	if batcher.Count() != 33 {
		t.Errorf("Expected 33 messages, got %d", batcher.Count())
	}
}

// TestBatcherQueryIDPreservation tests that QueryID is preserved through operations
func TestBatcherQueryIDPreservation(t *testing.T) {
	transport := NewTransport()
	receiveChan := make(chan *Batch, 10)
	transport.RegisterClient("test_client", receiveChan)

	queryID := "test_query_12345"
	batcher := NewBatcher(queryID, transport)

	// Multiple flush cycles
	for i := 0; i < 3; i++ {
		batcher.Append(Message{Message: "Test"})
		batcher.Flush()

		select {
		case batch := <-receiveChan:
			if batch.QueryID != queryID {
				t.Errorf("Batch %d: QueryID = %q, want %q", i, batch.QueryID, queryID)
			}
		case <-time.After(50 * time.Millisecond):
			t.Fatalf("Timeout waiting for batch %d", i)
		}
	}
}
