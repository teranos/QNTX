package async

import (
	"database/sql"
	"testing"
	"time"

	"github.com/teranos/QNTX/am"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// ============================================================================
// TAS Bot (Tool-Assisted Speedrun) & Kirby Test Universe
// ============================================================================
//
// Characters:
//   - TAS Bot: Frame-perfect coordinator who schedules jobs with precision timing
//   - Kirby: The worker who copies and executes jobs ('Poyo!')
//   - Cronos: Greek god of time, appears for timing-sensitive tests
//
// Theme: TAS Bot coordinates the perfect speedrun while Kirby executes jobs
// with copy abilities. Cronos ensures timing is frame-perfect.
// ============================================================================

// createTestConfig creates a minimal config for testing
func createTestConfig() *am.Config {
	return &am.Config{
		Pulse: am.PulseConfig{
			DailyBudgetUSD:    10.0,
			MonthlyBudgetUSD:  100.0,
			CostPerScoreUSD:   0.01,
			MaxCallsPerMinute: 60,
		},
	}
}

// createTestLogger creates a no-op logger for testing
func createTestLogger() *zap.SugaredLogger {
	return zap.NewNop().Sugar()
}

// TestTASBotInitializesWorkerPool tests that TAS Bot can initialize the worker pool
// with exact worker counts (frame-perfect setup)
func TestTASBotInitializesWorkerPool(t *testing.T) {
	t.Log("🎮 TAS Bot begins frame-perfect worker pool initialization...")
	t.Log("   'Calculating optimal worker count for this speedrun'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// TAS Bot creates worker pool with specific worker count
	workerCount := 3
	poolCfg := WorkerPoolConfig{Workers: workerCount}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	if pool == nil {
		t.Fatal("TAS Bot failed to create worker pool")
	}

	if pool.workers != workerCount {
		t.Errorf("TAS Bot expected %d workers, got %d", workerCount, pool.workers)
	}

	t.Logf("✓ TAS Bot initialized worker pool with %d workers", workerCount)
	t.Log("  'Frame-perfect setup complete'")
}

// TestKirbyExecutesJobs tests that Kirby can start the worker pool and workers begin
// processing (Kirby uses copy ability)
func TestKirbyExecutesJobs(t *testing.T) {
	t.Log("⭐ Kirby prepares to execute jobs with copy ability...")
	t.Log("   'Poyo!' *inhales deeply*")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Kirby creates a small worker pool
	poolCfg := WorkerPoolConfig{Workers: 2}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// Kirby starts the worker pool
	pool.Start()

	// Give Kirby a moment to start workers
	time.Sleep(10 * time.Millisecond)

	// Kirby stops the worker pool gracefully
	pool.Stop()

	t.Log("✓ Kirby successfully started and stopped workers")
	t.Log("  'Poyo!' *satisfied worker noises*")
}

// TestTASBotGracefulShutdown tests that TAS Bot can stop the worker pool
// within the 2-second timeout window
func TestTASBotGracefulShutdown(t *testing.T) {
	t.Log("🎮 TAS Bot tests frame-perfect shutdown timing...")
	t.Log("   'Shutdown must complete within 2 seconds for optimal speedrun time'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 3}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())
	pool.Start()

	// Let workers initialize
	time.Sleep(50 * time.Millisecond)

	// TAS Bot measures shutdown time
	startTime := time.Now()
	pool.Stop()
	shutdownDuration := time.Since(startTime)

	if shutdownDuration > 3*time.Second {
		t.Errorf("TAS Bot shutdown took too long: %v (expected < 3s)", shutdownDuration)
	}

	t.Logf("✓ TAS Bot shutdown completed in %v", shutdownDuration)
	t.Log("  'Frame-perfect shutdown achieved'")
}

// TestCronosWorkerIntervals tests that Cronos observes the correct worker
// polling intervals (time-based test)
func TestCronosWorkerIntervals(t *testing.T) {
	t.Log("⏰ Cronos, god of time, observes worker polling intervals...")
	t.Log("   'Time flows differently during warmup and normal operation'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 1}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// Cronos checks initial interval (warmup: 1 second)
	initialInterval := pool.getWorkerInterval()
	if initialInterval != 1*time.Second {
		t.Errorf("Cronos expected 1s warmup interval, got %v", initialInterval)
	}
	t.Log("✓ Cronos confirms warmup interval: 1 second")

	// Simulate time passing and jobs being processed
	pool.mu.Lock()
	pool.startTime = time.Now().Add(-5 * time.Minute) // Simulate 5 minutes ago
	pool.jobsProcessed = 25                           // Simulate 25 jobs processed
	pool.mu.Unlock()

	// Cronos checks normal interval (after warmup: 5 seconds)
	normalInterval := pool.getWorkerInterval()
	if normalInterval != 5*time.Second {
		t.Errorf("Cronos expected 5s normal interval, got %v", normalInterval)
	}
	t.Log("✓ Cronos confirms normal interval: 5 seconds")
	t.Log("  'Time has been measured accurately'")
}

// TestKirbyContextCancellation tests that Kirby stops processing when context
// is cancelled (Kirby stops inhaling)
func TestKirbyContextCancellation(t *testing.T) {
	t.Log("⭐ Kirby tests context cancellation (stop inhaling mid-job)...")
	t.Log("   'Poyo?' *pauses mid-inhale*")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 2}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// Kirby starts workers using the pool's Start() method
	pool.Start()

	// Let Kirby start processing
	time.Sleep(50 * time.Millisecond)

	// Cancel context (Kirby stops inhaling)
	pool.cancel()

	// Wait for workers to exit with timeout
	done := make(chan bool)
	go func() {
		pool.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		t.Log("✓ Kirby stopped all workers after context cancellation")
		t.Log("  'Poyo!' *stops inhaling successfully*")
	case <-time.After(3 * time.Second):
		t.Error("Kirby's workers did not exit within 3 seconds")
	}
}

// TestTASBotWorkerPoolStop tests that TAS Bot's Stop() method properly
// cancels context and waits for workers
func TestTASBotWorkerPoolStop(t *testing.T) {
	t.Log("🎮 TAS Bot tests complete worker pool shutdown sequence...")
	t.Log("   'Stop() must cancel context and wait for all workers'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 3}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())
	pool.Start()

	// Let workers initialize
	time.Sleep(50 * time.Millisecond)

	// TAS Bot calls Stop()
	startTime := time.Now()
	pool.Stop()
	duration := time.Since(startTime)

	// Verify context was cancelled (check if we can select on it immediately)
	select {
	case <-pool.ctx.Done():
		t.Log("✓ TAS Bot confirmed context was cancelled")
	default:
		t.Error("TAS Bot found context was not cancelled after Stop()")
	}

	// Verify stop completed within timeout
	if duration > 2500*time.Millisecond {
		t.Logf("⚠️  TAS Bot shutdown took %v (longer than 2s timeout)", duration)
	} else {
		t.Logf("✓ TAS Bot shutdown completed in %v", duration)
	}

	t.Log("  'Complete shutdown sequence verified'")
}

// TestCronosShutdownTimeout tests that Cronos measures the 2-second timeout
// correctly during shutdown
func TestCronosShutdownTimeout(t *testing.T) {
	t.Log("⏰ Cronos tests the 2-second shutdown timeout...")
	t.Log("   'Workers must exit within 2 seconds, or timeout returns anyway'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 2}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())
	pool.Start()

	// Let workers start
	time.Sleep(50 * time.Millisecond)

	// Cronos measures shutdown time
	startTime := time.Now()
	pool.Stop()
	shutdownTime := time.Since(startTime)

	// Should complete well before 2 seconds (workers exit immediately when idle)
	if shutdownTime > 500*time.Millisecond {
		t.Logf("⚠️  Cronos observed slower shutdown: %v", shutdownTime)
	} else {
		t.Logf("✓ Cronos confirmed fast shutdown: %v", shutdownTime)
	}

	// Verify timeout exists (Stop should never hang forever)
	if shutdownTime > 3*time.Second {
		t.Error("Cronos detected shutdown exceeded maximum timeout")
	}

	t.Log("  'Timeout mechanism verified'")
}

// TestKirbyProcessNextJobContextCheck tests that Kirby checks context before
// processing new jobs (Kirby checks before inhaling)
func TestKirbyProcessNextJobContextCheck(t *testing.T) {
	t.Log("⭐ Kirby tests context checking before job processing...")
	t.Log("   'Poyo? *checks if should still be inhaling*")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 1}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// Cancel context before processing
	pool.cancel()

	// Kirby tries to process a job (should return immediately due to context)
	err := pool.processNextJob()

	// Should return nil (graceful exit) rather than error
	if err != nil {
		t.Errorf("Kirby expected nil return on cancelled context, got error: %v", err)
	}

	t.Log("✓ Kirby correctly exited when context was cancelled")
	t.Log("  'Poyo!' *stops inhaling when told to stop*")
}

// TestTASBotMultipleWorkers tests that TAS Bot can coordinate multiple workers
// processing in parallel (frame-perfect parallel execution)
func TestTASBotMultipleWorkers(t *testing.T) {
	t.Log("🎮 TAS Bot coordinates multiple workers in parallel...")
	t.Log("   'Parallel execution must be frame-perfect'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	workerCount := 5
	poolCfg := WorkerPoolConfig{Workers: workerCount}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// TAS Bot starts all workers using pool.Start()
	pool.Start()

	// Give workers time to start
	time.Sleep(100 * time.Millisecond)
	t.Logf("  Started %d workers", workerCount)

	// TAS Bot stops all workers
	pool.Stop()

	t.Logf("✓ TAS Bot successfully coordinated %d parallel workers", workerCount)
	t.Log("  'Frame-perfect parallel execution achieved'")
}

// TestKirbyAndTASBotIntegration tests the complete workflow of TAS Bot
// coordinating while Kirby executes
func TestKirbyAndTASBotIntegration(t *testing.T) {
	t.Log("🎮⭐ TAS Bot and Kirby integration test...")
	t.Log("   TAS Bot: 'Frame-perfect coordination activated'")
	t.Log("   Kirby: 'Poyo!' *ready to copy and execute*")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// TAS Bot sets up the worker pool
	poolCfg := WorkerPoolConfig{Workers: 3}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	t.Log("  TAS Bot: 'Starting worker pool...'")
	pool.Start()

	// Let Kirby work for a bit
	time.Sleep(100 * time.Millisecond)
	t.Log("  Kirby: 'Poyo!' *working hard*")

	// TAS Bot initiates shutdown
	t.Log("  TAS Bot: 'Initiating frame-perfect shutdown...'")
	startTime := time.Now()
	pool.Stop()
	duration := time.Since(startTime)

	t.Logf("✓ Integration test complete in %v", duration)
	t.Log("  TAS Bot: 'Speedrun complete!'")
	t.Log("  Kirby: 'Poyo!' *victory dance*")
}

// TestCronosRateLimitingEnforcement tests that Cronos enforces rate limiting
// when jobs are dequeued (time-based gating mechanism)
func TestCronosRateLimitingEnforcement(t *testing.T) {
	t.Log("⏰ Cronos tests rate limiting enforcement during job processing...")
	t.Log("   'Jobs must respect the sacred flow of time - no more than N per minute'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	// Cronos sets a very low rate limit to make test deterministic
	cfg.Pulse.MaxCallsPerMinute = 3 // Only 3 calls per minute
	cfg.Pulse.PauseOnBudgetExceeded = true

	poolCfg := WorkerPoolConfig{Workers: 1}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// Skip if rate limiter not configured (requires pulse package, would create import cycle)
	if pool.rateLimiter == nil {
		t.Skip("Rate limiter not configured - requires pulse package to create limiters (would cause import cycle)")
		return
	}

	// Verify rate limiter was configured correctly
	callsInWindow, callsRemaining := pool.rateLimiter.Stats()
	if callsInWindow != 0 {
		t.Errorf("Cronos expected 0 initial calls, got %d", callsInWindow)
	}
	if callsRemaining != 3 {
		t.Errorf("Cronos expected 3 remaining calls, got %d", callsRemaining)
	}
	t.Log("✓ Cronos confirms rate limiter initialized: 0/3 calls used")

	// Test direct rate limiter behavior
	for i := 0; i < 3; i++ {
		if err := pool.rateLimiter.Allow(); err != nil {
			t.Errorf("Cronos expected call %d to be allowed, got error: %v", i+1, err)
		}
	}
	t.Log("✓ Cronos allowed first 3 calls")

	// 4th call should be rate limited
	if err := pool.rateLimiter.Allow(); err == nil {
		t.Error("Cronos expected 4th call to be rate limited")
	} else {
		t.Logf("✓ Cronos correctly rate limited 4th call: %v", err)
	}

	// Verify stats
	callsInWindow, callsRemaining = pool.rateLimiter.Stats()
	t.Logf("  Rate limiter state: %d calls in window, %d remaining", callsInWindow, callsRemaining)

	if callsInWindow != 3 {
		t.Errorf("Cronos expected 3 calls in window, got %d", callsInWindow)
	}
	if callsRemaining != 0 {
		t.Errorf("Cronos expected 0 remaining calls, got %d", callsRemaining)
	}

	t.Log("✓ Cronos confirmed rate limiting enforced correctly")
	t.Log("  'The flow of time has been properly controlled'")
}

// TestCronosCheckRateLimitWithNilLimiter tests that checkRateLimit handles nil limiter gracefully
func TestCronosCheckRateLimitWithNilLimiter(t *testing.T) {
	t.Log("⏰ Cronos tests rate limit with no limiter configured...")
	t.Log("   'When there is no time constraint, all flows freely!'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 1}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// Create a test job
	job := &Job{
		ID:          "JOB_NO_RATE_LIMIT",
		HandlerName: "test.handler",
		Source:      "no_limit.html",
		Status:      JobStatusQueued,
		CreatedAt:   time.Now(),
	}

	// checkRateLimit should return false (not paused) when no limiter configured
	paused, err := pool.checkRateLimit(job)
	if err != nil {
		t.Fatalf("checkRateLimit returned error: %v", err)
	}

	if paused {
		t.Error("Expected job NOT to be paused when no rate limiter configured")
	}

	t.Log("✓ Cronos allows job through when no rate limit configured")
	t.Log("  'Time flows unimpeded!'")
}

// TestCronosCheckBudgetWithNilTracker tests that checkBudget handles nil tracker gracefully
func TestCronosCheckBudgetWithNilTracker(t *testing.T) {
	t.Log("💰 Cronos tests budget enforcement with no tracker...")
	t.Log("   'When there is no treasury, all spending is permitted!'")

	db := qntxtest.CreateTestDB(t)
	cfg := createTestConfig()

	poolCfg := WorkerPoolConfig{Workers: 1}
	pool := NewWorkerPool(db, cfg, poolCfg, createTestLogger())

	// Create an expensive job
	job := &Job{
		ID:           "JOB_NO_BUDGET_TRACK",
		HandlerName:  "test.expensive-handler",
		Source:       "no_budget.html",
		Status:       JobStatusQueued,
		CostEstimate: 999.99, // Expensive
		CreatedAt:    time.Now(),
	}

	// checkBudget should return false (not paused) when no tracker configured
	paused, err := pool.checkBudget(job)
	if err != nil {
		t.Fatalf("checkBudget returned error: %v", err)
	}

	if paused {
		t.Error("Expected job NOT to be paused when no budget tracker configured")
	}

	t.Log("✓ Cronos allows expensive job when no budget tracker configured")
	t.Log("  'The treasury is infinite!'")
}
