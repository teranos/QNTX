package budget

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/teranos/QNTX/errors"
)

// Limiter enforces max calls per time window using sliding window algorithm
type Limiter struct {
	maxCallsPerMinute int
	window            time.Duration
	mu                sync.Mutex
	callTimes         []time.Time
	timeNow           func() time.Time // Injectable for testing
}

// NewLimiter creates a rate limiter with real time
func NewLimiter(maxCallsPerMinute int) *Limiter {
	return NewLimiterWithClock(maxCallsPerMinute, time.Now)
}

// NewLimiterWithClock creates a rate limiter with injectable clock (for testing)
func NewLimiterWithClock(maxCallsPerMinute int, timeNow func() time.Time) *Limiter {
	return &Limiter{
		maxCallsPerMinute: maxCallsPerMinute,
		window:            60 * time.Second, // 1 minute window
		callTimes:         make([]time.Time, 0, maxCallsPerMinute),
		timeNow:           timeNow,
	}
}

// Allow checks if a call is allowed under rate limits
// Returns error if rate limit exceeded
func (r *Limiter) Allow() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.timeNow()

	// Remove expired call timestamps (outside the window)
	r.removeExpiredCalls(now)

	// Check if we're at the limit
	if len(r.callTimes) >= r.maxCallsPerMinute {
		err := errors.Newf("rate limit exceeded: %d calls per minute (limit: %d)",
			len(r.callTimes), r.maxCallsPerMinute)
		err = errors.WithDetail(err, fmt.Sprintf("Current calls in window: %d", len(r.callTimes)))
		err = errors.WithDetail(err, fmt.Sprintf("Max calls per minute: %d", r.maxCallsPerMinute))
		err = errors.WithDetail(err, fmt.Sprintf("Remaining capacity: 0"))
		return err
	}

	// Record this call
	r.callTimes = append(r.callTimes, now)

	return nil
}

// Wait blocks until a call is allowed under rate limits
// Returns error if context is cancelled
func (r *Limiter) Wait(ctx context.Context) error {
	for {
		if err := r.Allow(); err == nil {
			return nil
		}

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Retry after short delay
		}
	}
}

// removeExpiredCalls removes call timestamps that are outside the sliding window
// Must be called with lock held
func (r *Limiter) removeExpiredCalls(now time.Time) {
	cutoff := now.Add(-r.window)

	// Count expired calls from front (timestamps are ordered)
	expired := 0
	for _, callTime := range r.callTimes {
		if !callTime.After(cutoff) {
			expired++
		} else {
			break
		}
	}

	r.callTimes = r.callTimes[expired:]
}

// Reset clears the rate limiter state
func (r *Limiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.callTimes = r.callTimes[:0]
}

// Stats returns current rate limiter statistics
func (r *Limiter) Stats() (callsInWindow int, remaining int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.timeNow()
	r.removeExpiredCalls(now)

	callsInWindow = len(r.callTimes)
	remaining = r.maxCallsPerMinute - callsInWindow
	if remaining < 0 {
		remaining = 0
	}

	return callsInWindow, remaining
}
