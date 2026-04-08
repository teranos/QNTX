package grpc

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/teranos/QNTX/errors"
)

// llmQueue is a priority-aware concurrency limiter.
// Lower priority value = higher priority (0 = interactive, 10 = background).
// Callers block until a slot opens, served in priority order.
type llmQueue struct {
	mu         sync.Mutex
	active     int
	maxSlots   int
	maxWaiters int
	cooldown   time.Duration // pause after each request before waking next waiter
	waiters    waiterHeap
}

func newLLMQueue(maxSlots int, maxWaiters int, cooldown time.Duration) *llmQueue {
	return &llmQueue{
		maxSlots:   maxSlots,
		maxWaiters: maxWaiters,
		cooldown:   cooldown,
	}
}

// Acquire blocks until a concurrency slot is available, respecting priority order.
// Returns context.Canceled or context.DeadlineExceeded if ctx is done while waiting.
func (q *llmQueue) Acquire(ctx context.Context, priority int32) error {
	q.mu.Lock()

	if q.active < q.maxSlots {
		q.active++
		q.mu.Unlock()
		return nil
	}

	// Reject if queue is full.
	if q.maxWaiters > 0 && q.waiters.Len() >= q.maxWaiters {
		q.mu.Unlock()
		return errors.Newf("LLM queue full (%d waiting, %d active)", q.waiters.Len(), q.active)
	}

	// Progressive backpressure: as queue fills, reject lower-priority requests.
	// At depth 10 reject priority >= 10 (background), at 15 reject >= 9, etc.
	depth := q.waiters.Len()
	if depth >= 10 {
		cutoff := int32(12 - depth/5)
		if priority >= cutoff {
			q.mu.Unlock()
			return errors.Newf("LLM backpressure: priority %d rejected (queue depth %d, cutoff %d)", priority, depth, cutoff)
		}
	}

	// No slot available — wait in priority order.
	w := &waiter{
		priority: priority,
		ready:    make(chan struct{}),
	}
	heap.Push(&q.waiters, w)
	q.mu.Unlock()

	select {
	case <-w.ready:
		return nil
	case <-ctx.Done():
		q.mu.Lock()
		// If we were already signaled between ctx.Done and acquiring the lock,
		// the slot was given to us — release it so the next waiter can proceed.
		select {
		case <-w.ready:
			q.mu.Unlock()
			q.Release()
			return ctx.Err()
		default:
		}
		// Remove from heap.
		if w.index >= 0 && w.index < q.waiters.Len() {
			heap.Remove(&q.waiters, w.index)
		}
		q.mu.Unlock()
		return ctx.Err()
	}
}

// Release returns a concurrency slot. If there are waiters, pauses for the
// cooldown duration before waking the next one — gives the system breathing room
// between back-to-back inference runs.
func (q *llmQueue) Release() {
	if q.cooldown > 0 {
		time.Sleep(q.cooldown)
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if q.waiters.Len() > 0 {
		w := heap.Pop(&q.waiters).(*waiter)
		close(w.ready)
		return
	}

	q.active--
}

// Stats returns current active count and queue depth.
func (q *llmQueue) Stats() (active int, queued int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.active, q.waiters.Len()
}

// waiter represents a goroutine waiting for a concurrency slot.
type waiter struct {
	priority int32
	ready    chan struct{}
	index    int // heap index, maintained by heap.Interface
}

// waiterHeap is a min-heap of waiters ordered by priority (lower = higher priority).
type waiterHeap []*waiter

func (h waiterHeap) Len() int           { return len(h) }
func (h waiterHeap) Less(i, j int) bool { return h[i].priority < h[j].priority }
func (h waiterHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *waiterHeap) Push(x any) {
	w := x.(*waiter)
	w.index = len(*h)
	*h = append(*h, w)
}

func (h *waiterHeap) Pop() any {
	old := *h
	n := len(old)
	w := old[n-1]
	old[n-1] = nil
	w.index = -1
	*h = old[:n-1]
	return w
}
