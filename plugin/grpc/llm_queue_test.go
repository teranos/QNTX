package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMQueue_ImmediateAcquire(t *testing.T) {
	q := newLLMQueue(2)

	require.NoError(t, q.Acquire(context.Background(), 0))
	require.NoError(t, q.Acquire(context.Background(), 0))

	active, queued := q.Stats()
	assert.Equal(t, 2, active)
	assert.Equal(t, 0, queued)

	q.Release()
	q.Release()

	active, queued = q.Stats()
	assert.Equal(t, 0, active)
	assert.Equal(t, 0, queued)
}

func TestLLMQueue_BlocksWhenFull(t *testing.T) {
	q := newLLMQueue(1)

	require.NoError(t, q.Acquire(context.Background(), 0))

	// Second acquire should block
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := q.Acquire(ctx, 0)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLLMQueue_PriorityOrdering(t *testing.T) {
	q := newLLMQueue(1)

	// Fill the single slot
	require.NoError(t, q.Acquire(context.Background(), 0))

	// Queue two waiters: low priority first, then high priority
	var order []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(2)

	// Low priority (10) queued first
	go func() {
		defer wg.Done()
		_ = q.Acquire(context.Background(), 10)
		mu.Lock()
		order = append(order, "low")
		mu.Unlock()
		q.Release()
	}()

	// Small delay so low-priority is queued first
	time.Sleep(10 * time.Millisecond)

	// High priority (0) queued second
	go func() {
		defer wg.Done()
		_ = q.Acquire(context.Background(), 0)
		mu.Lock()
		order = append(order, "high")
		mu.Unlock()
		q.Release()
	}()

	// Let both goroutines park
	time.Sleep(10 * time.Millisecond)

	_, queued := q.Stats()
	assert.Equal(t, 2, queued)

	// Release the slot — high priority should go first
	q.Release()

	wg.Wait()

	assert.Equal(t, []string{"high", "low"}, order)
}

func TestLLMQueue_ContextCancelWhileWaiting(t *testing.T) {
	q := newLLMQueue(1)
	require.NoError(t, q.Acquire(context.Background(), 0))

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	var acquireErr error
	go func() {
		defer wg.Done()
		acquireErr = q.Acquire(ctx, 5)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()
	wg.Wait()

	assert.ErrorIs(t, acquireErr, context.Canceled)

	// Queue should be empty after cancellation
	_, queued := q.Stats()
	assert.Equal(t, 0, queued)

	// Original slot still held — release it
	q.Release()
	active, _ := q.Stats()
	assert.Equal(t, 0, active)
}
