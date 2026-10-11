package services

import (
	"container/heap"
	"context"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// llmQueue is a priority-aware concurrency limiter.
// Lower priority value = higher priority (0 = interactive, 10 = background).
// Every request takes its place in one line, ordered by priority, and is
// served from the front of it when a slot is free. Freeing a slot asks nobody
// whether anyone waits: it wakes the line, and the front takes it.
type llmQueue struct {
	mu         sync.Mutex
	turn       *sync.Cond // a slot freed, or someone left the line
	active     int
	maxSlots   int
	maxWaiters int
	cooldown   time.Duration // pause after each request before the line is woken
	waiters    waiterHeap
	logger     *zap.SugaredLogger // says a request waits, when one does
}

func newLLMQueue(maxSlots int, maxWaiters int, cooldown time.Duration, logger *zap.SugaredLogger) *llmQueue {
	q := &llmQueue{
		maxSlots:   maxSlots,
		maxWaiters: maxWaiters,
		cooldown:   cooldown,
		logger:     logger,
	}
	q.turn = sync.NewCond(&q.mu)
	return q
}

// Acquire blocks until a concurrency slot is available, respecting priority order.
// Returns context.Canceled or context.DeadlineExceeded if ctx is done while waiting.
func (q *llmQueue) Acquire(ctx context.Context, priority int32) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	w := &waiter{priority: priority}
	heap.Push(&q.waiters, w)
	if q.served(w) {
		return nil
	}

	// It waits. The depth is who waits ahead of and beside it, not itself.
	// A depth of 0 queues nobody.
	depth := q.waiters.Len() - 1
	if depth >= q.maxWaiters {
		q.leave(w)
		return errors.Newf("LLM queue full (%d waiting, %d active)", depth, q.active)
	}

	// Progressive backpressure: as queue fills, reject lower-priority requests.
	// At depth 10 reject priority >= 10 (background), at 15 reject >= 9, etc.
	if depth >= 10 {
		cutoff := int32(12 - depth/5)
		if priority >= cutoff {
			q.leave(w)
			return errors.Newf("LLM backpressure: priority %d rejected (queue depth %d, cutoff %d)", priority, depth, cutoff)
		}
	}

	q.logger.Infow("LLM request queued", "priority", priority, "active", q.active, "queued", depth+1)

	// A context that ends wakes the line, so its request can leave it.
	stop := context.AfterFunc(ctx, func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		q.turn.Broadcast()
	})
	defer stop()

	for {
		if err := ctx.Err(); err != nil {
			q.leave(w)
			return err
		}
		if q.served(w) {
			return nil
		}
		q.turn.Wait()
	}
}

// served gives w a slot when w is at the front of the line and a slot is
// free, and takes it out of the line. w is in the line, so the line has a
// front.
func (q *llmQueue) served(w *waiter) bool {
	if q.waiters[0] != w || q.active >= q.maxSlots {
		return false
	}
	heap.Remove(&q.waiters, w.index)
	q.active++
	return true
}

// leave takes w out of the line, and wakes the line: whoever is now at the
// front may be served.
func (q *llmQueue) leave(w *waiter) {
	heap.Remove(&q.waiters, w.index)
	q.turn.Broadcast()
}

// Release returns a concurrency slot, after the cooldown — breathing room
// between back-to-back inference runs — and wakes the line.
func (q *llmQueue) Release() {
	// A cooldown of 0 sleeps for 0.
	time.Sleep(q.cooldown)

	q.mu.Lock()
	defer q.mu.Unlock()
	q.active--
	q.turn.Broadcast()
}

// Stats returns current active count and queue depth.
func (q *llmQueue) Stats() (active int, queued int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.active, q.waiters.Len()
}

// waiter is a request in the line for a concurrency slot.
type waiter struct {
	priority int32
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
	w, ok := x.(*waiter)
	if !ok {
		return
	}
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
