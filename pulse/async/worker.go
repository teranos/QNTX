package async

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/QNTX/sym"
	"go.uber.org/zap"
)

const (
	// MaxOrphanedJobsToRecover limits how many orphaned jobs we'll attempt to recover
	// on startup to prevent overwhelming the system after a crash
	MaxOrphanedJobsToRecover = 1000
)

// BudgetTracker interface defines budget tracking operations
type BudgetTracker interface {
	CheckBudget(estimatedCost float64) error
	GetStatus() (*budget.Status, error)
}

// RateLimiter interface defines rate limiting operations
type RateLimiter interface {
	Allow() error
	Stats() (callsInWindow int, callsRemaining int)
}

// pulseLogger wraps zap.SugaredLogger with special methods for Pulse operations
// Uses different log levels to create visual distinction:
// - DEBUG level → STARTING (✿ Opening operations)
// - WARN level → CLOSING (❀ Closing operations)
// - INFO level → PULSE (general worker/daemon operations)
type pulseLogger struct {
	*zap.SugaredLogger
}

// Starting logs an Opening (✿) event - uses DEBUG level for "STARTING" appearance
func (l pulseLogger) Starting(msg string, keysAndValues ...interface{}) {
	l.Debugw("✿ "+msg, keysAndValues...)
}

// Closing logs a Closing (❀) event - uses WARN level for "CLOSING" appearance
func (l pulseLogger) Closing(msg string, keysAndValues ...interface{}) {
	l.Warnw("❀ "+msg, keysAndValues...)
}

// Pulse logs general Pulse/worker operations - uses INFO level
func (l pulseLogger) Pulse(msg string, keysAndValues ...interface{}) {
	l.Infow(msg, keysAndValues...)
}

// MaxRetries is the maximum number of retry attempts for failed jobs
const MaxRetries = 2

type JobExecutor interface {
	Execute(ctx context.Context, job *Job) error
}

// WorkerPool manages a pool of workers that process async IX jobs
type WorkerPool struct {
	queue         *Queue
	budgetTracker BudgetTracker    // Budget tracking (optional - can be nil for tests)
	rateLimiter   RateLimiter      // Rate limiting (optional - can be nil for tests)
	db            *sql.DB
	config        *am.Config
	poolConfig    WorkerPoolConfig // Store pool configuration for graceful start timing
	workers       int
	parentCtx     context.Context    // Parent context from which worker context is derived
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	executor      JobExecutor
	jobsProcessed int         // Track jobs processed for gradual startup
	activeWorkers int         // Track currently active workers (executing jobs)
	startTime     time.Time   // Track when daemon started
	logger        pulseLogger // Structured logger for Pulse operations (shows STARTING/CLOSING levels)
	mu            sync.Mutex
}

// WorkerPoolConfig contains configuration for the worker pool
type WorkerPoolConfig struct {
	Workers            int           `json:"workers"`               // Number of concurrent workers
	PollInterval       time.Duration `json:"poll_interval"`         // How often to check for new jobs
	PauseOnBudget      bool          `json:"pause_on_budget"`       // Pause jobs when budget exceeded
	GracefulStartPhase time.Duration `json:"graceful_start_phase"`  // Duration of each graceful start phase (default: 5min, test: 10s)
}

// DefaultWorkerPoolConfig returns sensible defaults
func DefaultWorkerPoolConfig() WorkerPoolConfig {
	return WorkerPoolConfig{
		Workers:            1,               // Single worker to avoid race conditions initially
		PollInterval:       5 * time.Second, // Check for jobs every 5 seconds
		PauseOnBudget:      true,            // Pause when budget exceeded
		GracefulStartPhase: 5 * time.Minute, // 5min per phase = 15min total graceful start
	}
}

// NewWorkerPool creates a new worker pool with an empty handler registry.
// IMPORTANT: Callers must register handlers before calling Start().
//
// Phase 2 (Context Propagation): Worker pool now accepts parent context from server
// instead of creating its own. This enables proper shutdown coordination:
// - Server cancels root context during shutdown
// - Workers detect cancellation via ctx.Done() checks
// - Jobs checkpoint progress and exit cleanly
func NewWorkerPool(db *sql.DB, cfg *am.Config, poolCfg WorkerPoolConfig, logger *zap.SugaredLogger) *WorkerPool {
	return NewWorkerPoolWithContext(context.Background(), db, cfg, poolCfg, logger)
}

// NewWorkerPoolWithContext creates a worker pool with a custom context.
// Useful for tests and situations where you need to control the lifecycle.
// Uses nil budget/rate limiters - callers can use NewWorkerPoolWithRegistry for full control.
func NewWorkerPoolWithContext(ctx context.Context, db *sql.DB, cfg *am.Config, poolCfg WorkerPoolConfig, logger *zap.SugaredLogger) *WorkerPool {
	registry := NewHandlerRegistry()
	return NewWorkerPoolWithRegistry(ctx, db, cfg, poolCfg, logger, registry, nil, nil)
}

// NewWorkerPoolWithRegistry creates a worker pool with a custom handler registry.
// Use this when you need to:
// - Register custom job handlers
// - Configure stream broadcasting for WebSocket LLM streaming
// - Override default handler behavior
//
// Note: budgetTracker and rateLimiter can be nil for simple setups or tests.
func NewWorkerPoolWithRegistry(ctx context.Context, db *sql.DB, cfg *am.Config, poolCfg WorkerPoolConfig, logger *zap.SugaredLogger, registry *HandlerRegistry, budgetTracker BudgetTracker, rateLimiter RateLimiter) *WorkerPool {
	// Create child context so we can cancel workers independently if needed
	// But cancellation of parent context will also cancel child
	workerCtx, cancel := context.WithCancel(ctx)

	// Wrap logger with pulse-specific methods
	pLogger := pulseLogger{logger.Named("pulse")}

	// Create executor backed by handler registry
	executor := NewRegistryExecutor(registry, nil) // No fallback - all job types should be registered

	return &WorkerPool{
		queue:         NewQueue(db),
		budgetTracker: budgetTracker,
		rateLimiter:   rateLimiter,
		db:            db,
		config:        cfg,
		poolConfig:    poolCfg, // Store for graceful start timing
		workers:       poolCfg.Workers,
		parentCtx:     ctx,      // Store parent context for context recreation
		ctx:           workerCtx,
		cancel:        cancel,
		executor:      executor,
		logger:        pLogger,
	}
}

// Start begins processing jobs with the worker pool
// ✿ Opening: Recover orphaned jobs before starting workers
func (wp *WorkerPool) Start() {
	wp.mu.Lock()

	// Check if context was cancelled (after Stop()) - if so, create new one
	// This must happen BEFORE spawning workers to avoid races
	select {
	case <-wp.ctx.Done():
		// Context cancelled - create new child context from parent
		wp.ctx, wp.cancel = context.WithCancel(wp.parentCtx)
		wp.logger.Starting("Recreated worker context after previous shutdown")
	default:
		// Context still active
	}

	wp.startTime = time.Now()
	wp.jobsProcessed = 0
	wp.mu.Unlock()

	// ✿ Opening: Graceful start - recover jobs orphaned by server crash
	if err := wp.recoverOrphanedJobs(); err != nil {
		wp.logger.SugaredLogger.Warnw("Failed to recover orphaned jobs", "error", err)
		// Continue starting workers even if recovery fails
	}

	// Check memory pressure and warn if worker count may be too high
	if warning := wp.checkMemoryPressure(); warning != "" {
		wp.logger.SugaredLogger.Warnw("Memory pressure warning", "warning", warning, "workers", wp.workers)
	}

	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
}

// recoverOrphanedJobs finds jobs stuck in "running" state and re-queues them gradually
// This handles ungraceful shutdowns (crash, kill -9, power loss)
//
// ✿ Opening Strategy:
// - Re-queue orphaned jobs gradually over 15 minutes (not all at once)
// - Respects pulse budgets and rate limits during recovery
// - Prevents system overload after crash
// - Prioritizes jobs with checkpoints (can resume faster)
func (wp *WorkerPool) recoverOrphanedJobs() error {
	// Find all jobs that are still marked as "running"
	runningStatus := JobStatusRunning
	orphanedJobs, err := wp.queue.store.ListJobs(&runningStatus, MaxOrphanedJobsToRecover)
	if err != nil {
		return fmt.Errorf("failed to list running jobs: %w", err)
	}

	if len(orphanedJobs) == 0 {
		return nil // No orphaned jobs
	}

	wp.logger.Starting("Opening - found orphaned jobs from previous crash", "count", len(orphanedJobs))

	// Strategy: Super gradual warm start to avoid overwhelming the system
	// Phase 0 (Immediate): First job only
	// Phase 1 (0-10s): 1 job per second for next 9 jobs (jobs 2-10)
	// Phase 2 (10s-15min): Remaining jobs spread over 15 minutes

	if len(orphanedJobs) == 0 {
		return nil
	}

	// Recover first job immediately
	if err := wp.requeueOrphanedJob(orphanedJobs[0]); err != nil {
		wp.logger.SugaredLogger.Warnw("Failed to recover orphaned job", "job_id", orphanedJobs[0].ID, "error", err)
	} else {
		wp.logger.Starting("Immediately recovered first job", "current", 1, "total", len(orphanedJobs))
	}

	// Gradual recovery for remaining jobs in background
	if len(orphanedJobs) > 1 {
		wp.logger.Starting("Will gradually recover remaining jobs (warm start over 15 minutes)", "count", len(orphanedJobs)-1)
		go wp.gradualRecovery(orphanedJobs[1:])
	}

	return nil
}

// requeueOrphanedJob re-queues a single orphaned job
func (wp *WorkerPool) requeueOrphanedJob(job *Job) error {
	job.Status = JobStatusQueued
	job.Error = "" // Clear any stale error message

	if err := wp.queue.UpdateJob(job); err != nil {
		return fmt.Errorf("failed to update recovered job %s: %w", job.ID, err)
	}

	wp.logger.Starting("Recovered orphaned job", "job_id", job.ID, "handler", job.HandlerName)
	return nil
}

// gradualRecovery re-queues orphaned jobs gradually over 15 minutes
// This prevents overwhelming the system after a crash
//
// Warm Start Strategy:
// - Phase 1 (0-10s): Jobs 2-10 at 1 job per second (9 jobs total)
// - Phase 2 (10s-15min): Remaining jobs spread over 15 minutes
func (wp *WorkerPool) gradualRecovery(jobs []*Job) {
	if len(jobs) == 0 {
		return
	}

	startTime := time.Now()

	// Calculate phase durations (configurable for testing)
	warmStartDuration := 10 * time.Second
	slowStartDuration := 15 * time.Minute
	if wp.poolConfig.GracefulStartPhase > 0 {
		warmStartDuration = wp.poolConfig.GracefulStartPhase / 5
		slowStartDuration = wp.poolConfig.GracefulStartPhase * 3
	}

	// Warm start: first 9 jobs (or fewer if less available)
	warmStartLimit := min(9, len(jobs))
	warmStartInterval := warmStartDuration / time.Duration(warmStartLimit)
	wp.logger.Starting("Warm start phase", "count", warmStartLimit, "interval", warmStartInterval)

	warmRecovered := wp.recoverJobsWithInterval(jobs[:warmStartLimit], warmStartInterval, "warm start")
	wp.logger.Starting("Warm start complete", "recovered", warmRecovered, "duration", time.Since(startTime))

	// Slow start: remaining jobs
	remainingJobs := jobs[warmStartLimit:]
	if len(remainingJobs) == 0 {
		wp.logger.Starting("All jobs recovered during warm start")
		return
	}

	slowStartInterval := slowStartDuration / time.Duration(len(remainingJobs))
	wp.logger.Starting("Slow start phase", "count", len(remainingJobs), "interval", slowStartInterval)

	slowRecovered := wp.recoverJobsWithInterval(remainingJobs, slowStartInterval, "slow start")
	wp.logger.Starting("Gradual recovery complete", "recovered", warmRecovered+slowRecovered, "total", len(jobs), "duration", time.Since(startTime))
}

// Stop gracefully stops the worker pool
// ❀ Closing: Workers checkpoint and exit cleanly on context cancellation
// Uses a 30-second timeout to allow jobs to checkpoint without blocking indefinitely
func (wp *WorkerPool) Stop() {
	wp.cancel()

	// Wait for workers to checkpoint and exit (with generous timeout)
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()

	timeout := 30 * time.Second // Generous timeout for checkpoint completion
	select {
	case <-done:
		wp.logger.Pulse("❀ WorkerPool.Stop() complete - all workers exited cleanly")
	case <-time.After(timeout):
		wp.logger.Closing("WorkerPool.Stop() timeout - workers may still be checkpointing", "timeout", timeout)
		// Workers will continue checkpointing in background, but we return to avoid blocking shutdown
	}
}

// worker processes jobs from the queue
func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	// Start with slow ramp-up: 1 second between jobs (or PollInterval if configured)
	interval := wp.getWorkerInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Error backoff state
	errorCount := 0
	const maxConsecutiveErrors = 5
	backoffDuration := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-wp.ctx.Done():
			return
		case <-ticker.C:
			// Try to dequeue and process a job
			if err := wp.processNextJob(); err != nil {
				// Check if error is due to shutdown (context cancelled or database closed)
				select {
				case <-wp.ctx.Done():
					// Context cancelled - exit silently without logging
					return
				default:
					// Check if error is due to database being closed during shutdown
					if errors.Is(err, sql.ErrConnDone) {
						// Database closed during shutdown - exit silently
						return
					}
					// Real error - log it and apply backoff
					errorCount++
					wp.logger.SugaredLogger.Errorw("Worker error processing job",
						"worker_id", id,
						"error", err,
						"consecutive_errors", errorCount)

					// Apply exponential backoff after multiple consecutive errors
					if errorCount >= maxConsecutiveErrors {
						wp.logger.SugaredLogger.Warnw("Worker backing off due to consecutive errors",
							"worker_id", id,
							"backoff", backoffDuration,
							"consecutive_errors", errorCount)
						time.Sleep(backoffDuration)
						// Exponential backoff: double each time, cap at maxBackoff
						backoffDuration = min(backoffDuration*2, maxBackoff)
					}
				}
			} else {
				// Success - reset error backoff
				if errorCount > 0 {
					wp.logger.SugaredLogger.Infow("Worker recovered from errors",
						"worker_id", id,
						"previous_error_count", errorCount)
				}
				errorCount = 0
				backoffDuration = time.Second
			}

			// Update ticker interval based on startup phase
			newInterval := wp.getWorkerInterval()
			if newInterval != interval {
				ticker.Reset(newInterval)
				interval = newInterval
			}
		}
	}
}

// getWorkerInterval returns the current worker polling interval
// Starts at 1 second for gradual ramp-up, increases to 5 seconds after warmup
// If PollInterval is explicitly configured, use that instead of gradual ramp-up
func (wp *WorkerPool) getWorkerInterval() time.Duration {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	// If PollInterval is explicitly configured (non-zero), use that for all phases
	if wp.poolConfig.PollInterval > 0 {
		return wp.poolConfig.PollInterval
	}

	// Otherwise, use gradual ramp-up logic for production
	// Warmup period: first 20 jobs OR first 2 minutes, use 1-second intervals
	elapsed := time.Since(wp.startTime)
	if wp.jobsProcessed < 20 || elapsed < 2*time.Minute {
		return 1 * time.Second // Slow startup
	}

	// After warmup, use normal 5-second interval
	return 5 * time.Second
}

// processNextJob gets the next job from the queue and processes it
func (wp *WorkerPool) processNextJob() error {
	// Check if worker pool is shutting down
	select {
	case <-wp.ctx.Done():
		return nil // Graceful shutdown - don't process new jobs
	default:
		// Continue processing
	}

	// Dequeue next job
	job, err := wp.queue.Dequeue()
	if err != nil {
		return fmt.Errorf("failed to dequeue job: %w", err)
	}

	if job == nil {
		// No jobs available
		return nil
	}

	// TODO(QNTX #70): Add system load check as third gate before job execution
	// Current gates: (1) Rate limiting, (2) Budget checking
	// Needed gate: (3) System resource availability
	//
	// INTEGRATION POINT for cooperative multi-process resource management:
	//
	// When running on shared infrastructure (beefy GPU server with other processes):
	// 1. Check system-wide GPU/CPU utilization BEFORE dequeuing job
	// 2. If system under load (GPU >70%), defer job execution with exponential backoff
	// 3. Pause job with reason "system_busy" instead of failing
	// 4. Emit telemetry: "System GPU at 85%, deferring job execution for 30s"
	//
	// Example implementation:
	// ```go
	// // Check system load (third gate)
	// if wp.sysMonitor != nil {
	//     sysLoad, err := wp.sysMonitor.GetCurrentLoad()
	//     if err == nil && sysLoad.GPUUtilizationPercent > wp.config.Pulse.MaxSystemGPUPercent {
	//         // System busy - pause and back off
	//         if err := wp.queue.PauseJob(job.ID, "system_busy"); err != nil {
	//             return fmt.Errorf("failed to pause job %s: %w", job.ID, err)
	//         }
	//         log.Printf("Job %s paused: system GPU at %.1f%% (max %.1f%%), deferring for %s",
	//             job.ID, sysLoad.GPUUtilizationPercent, wp.config.Pulse.MaxSystemGPUPercent, backoffDuration)
	//         time.Sleep(backoffDuration)  // Exponential backoff
	//         return nil
	//     }
	// }
	// ```
	//
	// BENEFIT: qntx automatically yields to other processes during contention
	// - Respects shared infrastructure resource quotas
	// - Enables multi-tenant deployment (multiple users on same GPU server)
	// - Prevents qntx from starving training jobs, inference services, etc.
	//
	// See QNTX #70 for full multi-process coordination design

	// Check rate limit BEFORE budget check
	// Rate limiting prevents API violations, budget prevents cost overruns
	if paused, err := wp.checkRateLimit(job); paused || err != nil {
		if err != nil {
			return fmt.Errorf("rate limit check failed for job %s: %w", job.ID, err)
		}
		return nil // Job paused, no error
	}

	// Check budget before processing
	if paused, err := wp.checkBudget(job); paused || err != nil {
		if err != nil {
			return fmt.Errorf("budget check failed for job %s: %w", job.ID, err)
		}
		return nil // Job paused, no error
	}

	// Update pulse state with current rate/budget stats
	wp.updateJobPulseState(job)

	// Track job for gradual startup
	wp.mu.Lock()
	wp.jobsProcessed++
	wp.mu.Unlock()

	// Check if parent job was deleted before executing child task
	if job.ParentJobID != "" {
		parent, err := wp.queue.GetJob(job.ParentJobID)
		if err != nil {
			// Parent was deleted - cancel this child task
			job.Cancel("parent job deleted")
			return wp.queue.UpdateJob(job)
		}
		if parent.Status == JobStatusFailed || parent.Status == JobStatusCancelled {
			// Parent failed or was cancelled - cancel this child task
			job.Cancel(fmt.Sprintf("parent job %s", parent.Status))
			return wp.queue.UpdateJob(job)
		}
	}

	// Track active worker count (increment before execution, decrement after)
	wp.mu.Lock()
	wp.activeWorkers++
	wp.mu.Unlock()
	defer func() {
		wp.mu.Lock()
		wp.activeWorkers--
		wp.mu.Unlock()
	}()

	// Execute the job
	if err := wp.executor.Execute(wp.ctx, job); err != nil {
		// ❀ Closing: Check if error is due to context cancellation
		select {
		case <-wp.ctx.Done():
			// Context was cancelled - requeue job with checkpoint intact (don't fail it)
			wp.logger.Closing("Job cancelled during execution, re-queuing with checkpoint", "job_id", job.ID)
			job.Status = JobStatusQueued
			if updateErr := wp.queue.UpdateJob(job); updateErr != nil {
				wp.logger.SugaredLogger.Errorw("Failed to re-queue cancelled job", "job_id", job.ID, "error", updateErr)
			}
			return nil // Return nil to avoid logging as error
		default:
			// Real error - fail the job
			return wp.queue.FailJob(job.ID, err)
		}
	}

	// Mark job as completed
	return wp.queue.CompleteJob(job.ID)
}

// checkRateLimit verifies the rate limit and pauses the job if exceeded.
// Returns true if job was paused (caller should return), false to continue.
func (wp *WorkerPool) checkRateLimit(job *Job) (paused bool, err error) {
	// If no rate limiter configured, skip rate limiting (tests, simple setups)
	if wp.rateLimiter == nil {
		return false, nil
	}

	if err := wp.rateLimiter.Allow(); err != nil {
		if pauseErr := wp.queue.PauseJob(job.ID, "rate_limited"); pauseErr != nil {
			return false, fmt.Errorf("failed to pause job %s: %w", job.ID, pauseErr)
		}
		// Log rate limit status for visibility
		callsInWindow, callsRemaining := wp.rateLimiter.Stats()
		wp.logger.SugaredLogger.Infow(fmt.Sprintf(sym.Pulse+" Rate limit reached - job paused | calls:%d/%d remaining:%d | job:%s",
			callsInWindow, callsInWindow+callsRemaining, callsRemaining, job.ID[:12]),
			"job_id", job.ID,
			"calls_in_window", callsInWindow,
			"calls_remaining", callsRemaining,
			"reason", "rate_limited")
		return true, nil
	}
	return false, nil
}

// checkBudget verifies budget availability and pauses/fails the job if exceeded.
// Returns true if job was paused/failed (caller should return), false to continue.
func (wp *WorkerPool) checkBudget(job *Job) (paused bool, err error) {
	// If no budget tracker configured, skip budget checks (tests, simple setups)
	if wp.budgetTracker == nil {
		return false, nil
	}

	estimatedCost := job.CostEstimate
	if err := wp.budgetTracker.CheckBudget(estimatedCost); err != nil {
		// Get budget status for detailed logging
		status, statusErr := wp.budgetTracker.GetStatus()
		if statusErr == nil {
			// Calculate total limits from spend + remaining
			dailyLimit := status.DailySpend + status.DailyRemaining
			monthlyLimit := status.MonthlySpend + status.MonthlyRemaining

			wp.logger.SugaredLogger.Infow(fmt.Sprintf(sym.Pulse+" Budget exceeded - job %s | daily:$%.2f/$%.2f monthly:$%.2f/$%.2f | job:%s",
				func() string {
					if wp.poolConfig.PauseOnBudget {
						return "paused"
					}
					return "failed"
				}(),
				status.DailySpend, dailyLimit,
				status.MonthlySpend, monthlyLimit,
				job.ID[:12]),
				"job_id", job.ID,
				"estimated_cost", estimatedCost,
				"daily_spend", status.DailySpend,
				"daily_remaining", status.DailyRemaining,
				"monthly_spend", status.MonthlySpend,
				"monthly_remaining", status.MonthlyRemaining,
				"reason", "budget_exceeded")
		}

		if wp.poolConfig.PauseOnBudget {
			if pauseErr := wp.queue.PauseJob(job.ID, "budget_exceeded"); pauseErr != nil {
				return false, fmt.Errorf("failed to pause job %s: %w", job.ID, pauseErr)
			}
			return true, nil
		}
		return true, wp.queue.FailJob(job.ID, err)
	}
	return false, nil
}

// updateJobPulseState updates the job with current rate limiter and budget stats.
func (wp *WorkerPool) updateJobPulseState(job *Job) {
	// If no budget/rate tracking configured, skip pulse state updates (tests, simple setups)
	if wp.budgetTracker == nil || wp.rateLimiter == nil {
		return
	}

	status, err := wp.budgetTracker.GetStatus()
	if err != nil {
		wp.logger.SugaredLogger.Warnw("Failed to get budget status", "error", err)
		return
	}

	callsInWindow, callsRemaining := wp.rateLimiter.Stats()
	job.UpdatePulseState(&PulseState{
		CallsThisMinute: callsInWindow,
		CallsRemaining:  callsRemaining,
		SpendToday:      status.DailySpend,
		SpendThisMonth:  status.MonthlySpend,
		BudgetRemaining: status.DailyRemaining,
		IsPaused:        false,
		PauseReason:     "",
	})
	if err := wp.queue.UpdateJob(job); err != nil {
		wp.logger.SugaredLogger.Warnw("Failed to update job pulse state", "error", err)
	}
}

// recoverJobsWithInterval recovers a batch of jobs with a delay between each.
// Returns the number of jobs successfully recovered.
func (wp *WorkerPool) recoverJobsWithInterval(jobs []*Job, interval time.Duration, phase string) int {
	recovered := 0
	for i, job := range jobs {
		select {
		case <-wp.ctx.Done():
			wp.logger.Closing("Gradual recovery cancelled during "+phase, "recovered", recovered, "total", len(jobs))
			return recovered
		default:
		}

		if err := wp.requeueOrphanedJob(job); err != nil {
			wp.logger.SugaredLogger.Warnw("Failed to recover job during "+phase, "job_id", job.ID, "error", err)
			continue
		}
		recovered++

		// Progress logging every 10 jobs
		if recovered%10 == 0 {
			wp.logger.Starting("Gradual recovery progress", "current", recovered, "total", len(jobs), "phase", phase)
		}

		// Wait before next job (unless it's the last one)
		if i < len(jobs)-1 {
			time.Sleep(interval)
		}
	}
	return recovered
}

// GetQueue returns the job queue (useful for enqueuing jobs)
func (wp *WorkerPool) GetQueue() *Queue {
	return wp.queue
}

// Workers returns the number of concurrent workers configured for this pool
func (wp *WorkerPool) Workers() int {
	return wp.workers
}

// Registry returns the handler registry for registering custom job handlers.
// Use this to register handlers before calling Start():
//
//	pool := async.NewWorkerPool(db, cfg, poolCfg, logger)
//	role.RegisterRoleHandlers(pool.Registry(), db, cfg, nil)
//	pool.Start()
func (wp *WorkerPool) Registry() *HandlerRegistry {
	if registryExec, ok := wp.executor.(*RegistryExecutor); ok {
		return registryExec.registry
	}
	return nil
}
