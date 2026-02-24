package grpc

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func makeEntry(i int) LogEntry {
	return LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Line:      fmt.Sprintf("line-%d", i),
		Source:    "stdout",
	}
}

func TestLogBuffer_RecentOnEmpty(t *testing.T) {
	buf := NewLogBuffer(10)
	assert.Nil(t, buf.Recent(10))
	assert.Nil(t, buf.Recent(0))
}

func TestLogBuffer_WriteAndRecent(t *testing.T) {
	buf := NewLogBuffer(10)

	for i := 0; i < 3; i++ {
		buf.Write(makeEntry(i))
	}

	entries := buf.Recent(3)
	require.Len(t, entries, 3)
	assert.Equal(t, "line-0", entries[0].Line)
	assert.Equal(t, "line-1", entries[1].Line)
	assert.Equal(t, "line-2", entries[2].Line)
}

func TestLogBuffer_RecentCapsAtCount(t *testing.T) {
	buf := NewLogBuffer(10)

	for i := 0; i < 3; i++ {
		buf.Write(makeEntry(i))
	}

	entries := buf.Recent(100)
	assert.Len(t, entries, 3)
}

func TestLogBuffer_RingWraparound(t *testing.T) {
	capacity := 5
	buf := NewLogBuffer(capacity)

	// Write capacity + 5 entries — first 5 should be evicted
	total := capacity + 5
	for i := 0; i < total; i++ {
		buf.Write(makeEntry(i))
	}

	entries := buf.Recent(capacity)
	require.Len(t, entries, capacity)

	// Should contain the last `capacity` entries in order
	for i := 0; i < capacity; i++ {
		expected := fmt.Sprintf("line-%d", total-capacity+i)
		assert.Equal(t, expected, entries[i].Line)
	}
}

func TestLogBuffer_SubscribeReceivesNewEntries(t *testing.T) {
	buf := NewLogBuffer(10)
	ch := buf.Subscribe()
	defer buf.Unsubscribe(ch)

	entry := makeEntry(42)
	buf.Write(entry)

	select {
	case got := <-ch:
		assert.Equal(t, "line-42", got.Line)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for entry on subscriber channel")
	}
}

func TestLogBuffer_UnsubscribeStopsDelivery(t *testing.T) {
	buf := NewLogBuffer(10)
	ch := buf.Subscribe()
	buf.Unsubscribe(ch)

	buf.Write(makeEntry(0))

	// Channel should be closed
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed after unsubscribe")
}

func TestLogBuffer_MultipleSubscribers(t *testing.T) {
	buf := NewLogBuffer(10)

	subs := make([]chan LogEntry, 3)
	for i := range subs {
		subs[i] = buf.Subscribe()
		defer buf.Unsubscribe(subs[i])
	}

	buf.Write(makeEntry(7))

	for i, ch := range subs {
		select {
		case got := <-ch:
			assert.Equal(t, "line-7", got.Line, "subscriber %d", i)
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timeout", i)
		}
	}
}

func TestLogBuffer_NonBlockingFanOut(t *testing.T) {
	buf := NewLogBuffer(10)

	// Subscriber with tiny buffer — will fill up immediately
	slow := buf.Subscribe()
	defer buf.Unsubscribe(slow)

	// Fast subscriber
	fast := buf.Subscribe()
	defer buf.Unsubscribe(fast)

	// Fill slow subscriber's buffer (64 capacity)
	for i := 0; i < 70; i++ {
		buf.Write(makeEntry(i))
	}

	// Fast subscriber should have received entries (drain to verify)
	drained := 0
	for {
		select {
		case <-fast:
			drained++
		default:
			goto done
		}
	}
done:
	assert.Greater(t, drained, 0, "fast subscriber should have received entries despite slow subscriber")
}

func TestLogBuffer_ConcurrentWrites(t *testing.T) {
	capacity := 100
	buf := NewLogBuffer(capacity)

	var wg sync.WaitGroup
	writers := 10
	perWriter := 100

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				buf.Write(LogEntry{
					Timestamp: time.Now(),
					Level:     "info",
					Line:      fmt.Sprintf("w%d-%d", workerID, i),
					Source:    "stdout",
				})
			}
		}(w)
	}

	wg.Wait()

	entries := buf.Recent(capacity)
	assert.Len(t, entries, capacity, "should have exactly capacity entries after overflow")

	// All lines should be non-empty (no zero-value entries)
	for i, e := range entries {
		assert.NotEmpty(t, e.Line, "entry %d should not be empty", i)
	}
}

// --- pluginLogger → LogBuffer integration ---

func TestPluginLogger_WritesToLogBuffer(t *testing.T) {
	buf := NewLogBuffer(10)
	logger := &pluginLogger{
		logger:    zap.NewNop().Sugar(),
		name:      "test-plugin",
		level:     "info",
		logBuffer: buf,
	}

	n, err := logger.Write([]byte("hello world\n"))
	require.NoError(t, err)
	assert.Equal(t, 12, n)

	entries := buf.Recent(1)
	require.Len(t, entries, 1)
	assert.Equal(t, "hello world", entries[0].Line)
	assert.Equal(t, "info", entries[0].Level)
	assert.Equal(t, "stdout", entries[0].Source)
}

func TestPluginLogger_JSONLevelExtraction(t *testing.T) {
	buf := NewLogBuffer(10)
	logger := &pluginLogger{
		logger:    zap.NewNop().Sugar(),
		name:      "test-plugin",
		level:     "info",
		logBuffer: buf,
	}

	logger.Write([]byte(`{"level":"warn","msg":"something happened"}` + "\n"))

	entries := buf.Recent(1)
	require.Len(t, entries, 1)
	assert.Equal(t, "warn", entries[0].Level)
}

func TestPluginLogger_StderrSource(t *testing.T) {
	buf := NewLogBuffer(10)
	logger := &pluginLogger{
		logger:    zap.NewNop().Sugar(),
		name:      "test-plugin",
		level:     "error", // stderr logger has level "error"
		logBuffer: buf,
	}

	logger.Write([]byte("panic: something broke\n"))

	entries := buf.Recent(1)
	require.Len(t, entries, 1)
	assert.Equal(t, "stderr", entries[0].Source)
	assert.Equal(t, "error", entries[0].Level)
}
