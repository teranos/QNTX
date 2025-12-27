package budget

import (
	"sync"
	"testing"
	"time"
)

// mockClock allows controlling time in tests
type mockClock struct {
	mu  sync.Mutex
	now time.Time
}

func newMockClock(now time.Time) *mockClock {
	return &mockClock{now: now}
}

func (m *mockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *mockClock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
}

// Test Case 1: Under Limit
// Given: Limiter configured for 10 calls/minute
// When: Making 5 calls within 1 minute
// Then: All calls should be allowed
func TestLimiter_UnderLimit(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(10, clock.Now)

	// Make 5 calls (under limit of 10)
	for i := 0; i < 5; i++ {
		if err := limiter.Allow(); err != nil {
			t.Errorf("Call %d: expected no error, got %v", i+1, err)
		}
		clock.Advance(1 * time.Second) // Advance time between calls
	}
}

// Test Case 2: At Limit
// Given: Limiter configured for 10 calls/minute
// When: Making exactly 10 calls within 1 minute
// Then: All calls should be allowed, 11th should be rejected
func TestLimiter_AtLimit(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(10, clock.Now)

	// Make exactly 10 calls (at limit)
	for i := 0; i < 10; i++ {
		if err := limiter.Allow(); err != nil {
			t.Errorf("Call %d: expected no error, got %v", i+1, err)
		}
		clock.Advance(100 * time.Millisecond) // Space out calls slightly
	}

	// 11th call should be rejected
	if err := limiter.Allow(); err == nil {
		t.Error("Call 11: expected rate limit error, got nil")
	}
}

// Test Case 3: Over Limit
// Given: Limiter configured for 10 calls/minute
// When: Making 15 calls within 1 minute
// Then: First 10 allowed, last 5 rejected
func TestLimiter_OverLimit(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(10, clock.Now)

	successCount := 0
	failureCount := 0

	// Try to make 15 calls rapidly
	for i := 0; i < 15; i++ {
		if err := limiter.Allow(); err == nil {
			successCount++
		} else {
			failureCount++
		}
		clock.Advance(10 * time.Millisecond) // Very short intervals
	}

	if successCount != 10 {
		t.Errorf("Expected 10 successful calls, got %d", successCount)
	}
	if failureCount != 5 {
		t.Errorf("Expected 5 failed calls, got %d", failureCount)
	}
}

// Test Case 4: Window Reset
// Given: Limiter at capacity (10/10 calls used)
// When: Waiting for window to expire (>60 seconds)
// Then: Next call should be allowed
func TestLimiter_WindowReset(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(10, clock.Now)

	// Fill up the limit
	for i := 0; i < 10; i++ {
		if err := limiter.Allow(); err != nil {
			t.Fatalf("Setup call %d failed: %v", i+1, err)
		}
		clock.Advance(100 * time.Millisecond)
	}

	// Next call should be rejected (at limit)
	if err := limiter.Allow(); err == nil {
		t.Error("Expected rate limit error before window reset")
	}

	// Advance time beyond 60 seconds to reset window
	clock.Advance(61 * time.Second)

	// Now call should succeed
	if err := limiter.Allow(); err != nil {
		t.Errorf("Expected call to succeed after window reset, got error: %v", err)
	}
}

// Test Case 5: Concurrent Safety
// Given: Limiter configured for 100 calls/minute
// When: 10 goroutines each making 20 calls (200 total)
// Then: Exactly 100 should succeed, 100 should fail
// And: No race conditions (use -race flag)
func TestLimiter_Concurrent(t *testing.T) {
	// Use real time for concurrency test
	limiter := NewLimiter(100)

	var wg sync.WaitGroup
	results := make(chan bool, 200)

	// Launch 10 goroutines, each making 20 calls
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				err := limiter.Allow()
				results <- (err == nil)
			}
		}()
	}

	wg.Wait()
	close(results)

	successCount := 0
	failureCount := 0
	for success := range results {
		if success {
			successCount++
		} else {
			failureCount++
		}
	}

	if successCount != 100 {
		t.Errorf("Expected exactly 100 successful calls, got %d", successCount)
	}
	if failureCount != 100 {
		t.Errorf("Expected exactly 100 failed calls, got %d", failureCount)
	}
}

// Test Case 6: Burst Handling (Sliding Window Semantics)
// Given: Sliding window limiter with 10 calls/minute limit
// When: Making 10 calls instantly, then waiting 60s, then 10 more
// Then: First batch allowed, second batch allowed after window expires
func TestLimiter_BurstHandling(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(10, clock.Now)

	// First burst: all 10 calls should succeed
	for i := 0; i < 10; i++ {
		if err := limiter.Allow(); err != nil {
			t.Errorf("Burst call %d failed: %v", i+1, err)
		}
	}

	// At this point, we're at limit (10/10 used in last 60s)
	if err := limiter.Allow(); err == nil {
		t.Error("Expected rate limit error when at capacity")
	}

	// Wait 30 seconds - still within 60s window, so still at limit
	clock.Advance(30 * time.Second)
	if err := limiter.Allow(); err == nil {
		t.Error("Expected rate limit error at 30s (still within window)")
	}

	// Wait another 31 seconds (61s total) - now calls from T=0 have expired
	clock.Advance(31 * time.Second)

	// Should be able to make 10 more calls now
	for i := 0; i < 10; i++ {
		if err := limiter.Allow(); err != nil {
			t.Errorf("Post-window call %d failed: %v", i+1, err)
		}
	}
}

// Test Case 7: Per-Minute Calculation
// Given: Limiter configured for 60 calls/minute
// When: Making 1 call per second for 60 seconds
// Then: All calls should succeed (distributed across window)
func TestLimiter_PerMinuteCalculation(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(60, clock.Now)

	// Make 60 calls at 1 per second
	for i := 0; i < 60; i++ {
		if err := limiter.Allow(); err != nil {
			t.Errorf("Call %d at second %d failed: %v", i+1, i, err)
		}
		clock.Advance(1 * time.Second)
	}
}

// Test Case 8: Reset Functionality
// Given: Limiter with some calls made
// When: Reset() is called
// Then: All call history cleared, can make full limit again
func TestLimiter_Reset(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(10, clock.Now)

	// Make 10 calls to fill limit
	for i := 0; i < 10; i++ {
		if err := limiter.Allow(); err != nil {
			t.Fatalf("Setup call %d failed: %v", i+1, err)
		}
	}

	// Should be at limit
	if err := limiter.Allow(); err == nil {
		t.Error("Expected rate limit error before reset")
	}

	// Reset the limiter
	limiter.Reset()

	// Should be able to make 10 more calls
	for i := 0; i < 10; i++ {
		if err := limiter.Allow(); err != nil {
			t.Errorf("Post-reset call %d failed: %v", i+1, err)
		}
	}
}

// Test Case 9: Error Messages
// Given: Rate limited call
// When: Checking error message
// Then: Error should contain useful information
func TestLimiter_ErrorMessage(t *testing.T) {
	clock := newMockClock(time.Now())
	limiter := NewLimiterWithClock(10, clock.Now)

	// Fill limit
	for i := 0; i < 10; i++ {
		limiter.Allow()
	}

	// Get rate limit error
	err := limiter.Allow()
	if err == nil {
		t.Fatal("Expected rate limit error")
	}

	// Error should mention rate limiting
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("Rate limit error has empty message")
	}
	// Could check for specific phrases like "rate limit", "calls per minute", etc.
	t.Logf("Rate limit error message: %s", errMsg)
}
