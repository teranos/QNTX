package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/internal/util"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/sym"
	"github.com/teranos/vanity-id"
)

// NOTE: Ticker is now domain-agnostic (Issue #152 resolved)
// The ticker uses pre-computed handler_name and payload from scheduled jobs.
// ATS code parsing is done once at job creation time (see ats_parser.go).
// Legacy jobs without handler_name fall back to parsing for backward compatibility.
//
// Future enhancement: priority-based scheduling using ATS expressions.

// ExecutionBroadcaster defines interface for broadcasting execution events
// This avoids circular dependency between schedule and server packages
type ExecutionBroadcaster interface {
	BroadcastPulseExecutionStarted(scheduledJobID, executionID, atsCode string)
	BroadcastPulseExecutionFailed(scheduledJobID, executionID, atsCode, errorMsg string, durationMs int)
}

// Ticker manages periodic execution of scheduled ATS jobs
// Runs every second to check for jobs that need execution
type Ticker struct {
	store           *Store
	queue           *async.Queue
	workerPool      *async.WorkerPool // For system metrics in ticker display
	broadcaster     ExecutionBroadcaster
	interval        time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	logger          *zap.SugaredLogger
	mu              sync.Mutex
	lastTickAt      time.Time
	ticksSinceStart int64
	lastActiveWork  int // Track last active work count to detect changes
}

// TickerConfig contains configuration for the Pulse ticker
type TickerConfig struct {
	Interval time.Duration // How often to check for scheduled jobs (default: 1 second)
}

// DefaultTickerConfig returns sensible defaults
func DefaultTickerConfig() TickerConfig {
	return TickerConfig{
		Interval: 1 * time.Second,
	}
}

// NewTicker creates a new Pulse ticker
// The ticker checks for scheduled jobs at the configured interval
func NewTicker(store *Store, queue *async.Queue, workerPool *async.WorkerPool, broadcaster ExecutionBroadcaster, cfg TickerConfig, logger *zap.SugaredLogger) *Ticker {
	return NewTickerWithContext(context.Background(), store, queue, workerPool, broadcaster, cfg, logger)
}

// NewTickerWithContext creates a ticker with a parent context
func NewTickerWithContext(ctx context.Context, store *Store, queue *async.Queue, workerPool *async.WorkerPool, broadcaster ExecutionBroadcaster, cfg TickerConfig, logger *zap.SugaredLogger) *Ticker {
	tickerCtx, cancel := context.WithCancel(ctx)

	return &Ticker{
		store:       store,
		queue:       queue,
		workerPool:  workerPool,
		broadcaster: broadcaster,
		interval:    cfg.Interval,
		ctx:         tickerCtx,
		cancel:      cancel,
		logger:      logger,
	}
}

// Start begins the ticker loop
func (t *Ticker) Start() {
	t.wg.Add(1)
	go t.run()
	t.logger.Infow(sym.Pulse+" Pulse ticker started", "interval", t.interval)
}

// Stop gracefully stops the ticker
func (t *Ticker) Stop() {
	t.cancel()
	t.wg.Wait()
	t.logger.Infow(sym.Pulse + " Pulse ticker stopped")
}

// run is the main ticker loop
func (t *Ticker) run() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case tickTime := <-ticker.C:
			t.mu.Lock()
			t.lastTickAt = tickTime
			t.ticksSinceStart++
			t.mu.Unlock()

			// Log time until next job
			t.logNextJobInfo(tickTime)

			if err := t.checkScheduledJobs(tickTime); err != nil {
				// Don't spam logs - log errors at warn level
				t.logger.Warnw(sym.Pulse+" Pulse tick error", "error", err, "tick", t.ticksSinceStart)
			}
		}
	}
}

// logNextJobInfo logs time until the next scheduled job
func (t *Ticker) logNextJobInfo(now time.Time) {
	nextJob, err := t.store.GetNextScheduledJob()
	if err != nil {
		t.logger.Warnw(sym.Pulse+" Failed to get next scheduled job", "error", err)
		return
	}

	// Get queue stats for activity indicator
	stats, err := t.queue.GetStats()
	if err != nil {
		t.logger.Warnw("Failed to get queue stats", "error", err)
		// Continue without stats
		stats = &async.QueueStats{}
	}

	// Create visual indicator based on active work (queued + running)
	activeWork := stats.Queued + stats.Running

	// Only log if active work count has changed
	t.mu.Lock()
	hasChanged := activeWork != t.lastActiveWork
	t.lastActiveWork = activeWork
	t.mu.Unlock()

	if !hasChanged {
		return // Skip logging if no change in queue activity
	}

	pulseSymbols := sym.Pulse
	if activeWork > 0 {
		// Add more pulse symbols based on work load (1 symbol per 5 jobs, max 60 symbols)
		numSymbols := (activeWork / 5) + 1 // 1-5 jobs = 1 symbol, 6-10 = 2, etc.
		if numSymbols > 60 {
			numSymbols = 60 // Cap at 60 symbols (300 jobs)
		}
		pulseSymbols = strings.Repeat(sym.Pulse+" ", numSymbols)
		pulseSymbols = strings.TrimSpace(pulseSymbols)
	}

	if nextJob == nil {
		if activeWork > 0 {
			t.logger.Infow(fmt.Sprintf("%s Pulse - no scheduled executions, %d jobs active", pulseSymbols, activeWork))
		} else {
			t.logger.Infow(pulseSymbols + " Pulse - no scheduled executions")
		}
		return
	}

	timeUntil := nextJob.NextRunAt.Sub(now)
	if timeUntil < 0 {
		timeUntil = 0
	}

	// Build enhanced ticker message with system metrics
	msg := fmt.Sprintf("%s Pulse - next scheduled execution '%s' in %s", pulseSymbols, nextJob.ATSCode, timeUntil.Round(time.Second))
	if activeWork > 0 {
		msg += fmt.Sprintf(", %d jobs active", activeWork)
	}

	// Add system metrics if worker pool is available
	if t.workerPool != nil {
		metrics := t.workerPool.GetSystemMetrics()
		msg += fmt.Sprintf(" │ Workers: %d/%d active │ Mem: %.1f/%.1fGB (%.0f%%)",
			metrics.WorkersActive, metrics.WorkersTotal,
			metrics.MemoryUsedGB, metrics.MemoryTotalGB, metrics.MemoryPercent)
	}

	t.logger.Infow(msg)
}

// checkScheduledJobs finds scheduled jobs ready to run and enqueues them
func (t *Ticker) checkScheduledJobs(now time.Time) error {
	// Query for active jobs that are due to run (with context for graceful cancellation)
	jobs, err := t.store.ListJobsDueContext(t.ctx, now)
	if err != nil {
		return fmt.Errorf("failed to list scheduled jobs: %w", err)
	}

	if len(jobs) == 0 {
		return nil // No jobs to run
	}

	// Process each job
	for _, job := range jobs {
		// Check for context cancellation before processing next job
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		default:
		}

		if err := t.executeScheduledJob(job, now); err != nil {
			t.logger.Errorw(sym.Pulse+" Failed to execute scheduled job",
				"job_id", job.ID,
				"ats_code", job.ATSCode,
				"error", err)
			// Continue with other jobs even if one fails
			continue
		}
	}

	return nil
}

// executeScheduledJob creates an async job for the scheduled job and updates next_run_at
func (t *Ticker) executeScheduledJob(scheduled *Job, now time.Time) error {
	startTime := time.Now()

	t.logger.Infow(fmt.Sprintf("%s Pulse executing scheduled job | job:%s", sym.Pulse, scheduled.ID[:8]),
		"job_id", scheduled.ID,
		"ats_code", scheduled.ATSCode,
		"handler_name", scheduled.HandlerName,
		"source_url", scheduled.SourceURL)

	// Create execution record
	execution := &Execution{
		ID:             id.GenerateExecutionID(),
		ScheduledJobID: scheduled.ID,
		Status:         ExecutionStatusRunning,
		StartedAt:      startTime.Format(time.RFC3339),
		CreatedAt:      startTime.Format(time.RFC3339),
		UpdatedAt:      startTime.Format(time.RFC3339),
	}

	execStore := NewExecutionStore(t.store.db)
	if err := execStore.CreateExecution(execution); err != nil {
		t.logger.Errorw(sym.Pulse+" Failed to create execution record",
			"job_id", scheduled.ID,
			"error", err)
		// Continue anyway - execution tracking is nice-to-have
	}

	// Broadcast execution started event
	if t.broadcaster != nil {
		t.broadcaster.BroadcastPulseExecutionStarted(scheduled.ID, execution.ID, scheduled.ATSCode)
	}

	// Enqueue the async job (domain-agnostic - uses pre-computed handler/payload)
	asyncJobID, err := t.enqueueAsyncJob(scheduled)

	// Calculate execution duration
	completedAt := time.Now()
	durationMs := int(completedAt.Sub(startTime).Milliseconds())
	execution.CompletedAt = util.Ptr(completedAt.Format(time.RFC3339))
	execution.DurationMs = &durationMs
	execution.UpdatedAt = completedAt.Format(time.RFC3339)

	if err != nil {
		// Execution failed
		execution.Status = ExecutionStatusFailed
		errorMsg := err.Error()
		execution.ErrorMessage = &errorMsg

		t.logger.Errorw(fmt.Sprintf(sym.Pulse+" Pulse FAILED: %s | job:%s exec:%s (%dms) | ERROR: %s",
			scheduled.ATSCode, scheduled.ID[:8], execution.ID[:8], durationMs, err.Error()),
			"job_id", scheduled.ID,
			"execution_id", execution.ID,
			"ats_code", scheduled.ATSCode,
			"error", err)

		// Broadcast execution failed event
		if t.broadcaster != nil {
			t.broadcaster.BroadcastPulseExecutionFailed(scheduled.ID, execution.ID, scheduled.ATSCode, errorMsg, durationMs)
		}
	} else {
		// Execution succeeded
		execution.Status = ExecutionStatusCompleted
		execution.AsyncJobID = &asyncJobID
		summary := fmt.Sprintf("Created async job %s", asyncJobID)
		execution.ResultSummary = &summary

		// Calculate next run time
		nextRun := now.Add(time.Duration(scheduled.IntervalSeconds) * time.Second)
		nextRunRelative := time.Until(nextRun).Round(time.Minute)

		t.logger.Infow(fmt.Sprintf(sym.Pulse+" Pulse OK: %s → async:%s | job:%s exec:%s | next in %v (%dms)",
			scheduled.ATSCode, asyncJobID[:8], scheduled.ID[:8], execution.ID[:8], nextRunRelative, durationMs),
			"job_id", scheduled.ID,
			"execution_id", execution.ID,
			"async_job_id", asyncJobID,
			"ats_code", scheduled.ATSCode,
			"next_run_at", nextRun.Format(time.RFC3339))

		// Update the scheduled job with next run time
		if err := t.store.UpdateJobAfterExecution(scheduled.ID, now, asyncJobID, nextRun); err != nil {
			return fmt.Errorf("failed to update scheduled job: %w", err)
		}
	}

	// Update execution record with final status
	if err := execStore.UpdateExecution(execution); err != nil {
		t.logger.Errorw(sym.Pulse+" Failed to update execution record",
			"execution_id", execution.ID,
			"error", err)
		// Not critical - continue
	}

	return nil
}

// resolvePayloadLastRun checks if the payload contains "since":"last_run" and
// replaces it with the actual last_run_at timestamp from the scheduled job.
// This enables incremental processing for scheduled jobs.
func (t *Ticker) resolvePayloadLastRun(scheduled *Job) []byte {
	if scheduled.Payload == nil || len(scheduled.Payload) == 0 {
		return scheduled.Payload
	}

	// Check if payload contains "last_run" (quick check before parsing)
	if !strings.Contains(string(scheduled.Payload), `"last_run"`) {
		return scheduled.Payload
	}

	// Parse payload to check/modify since field
	var payloadMap map[string]interface{}
	if err := json.Unmarshal(scheduled.Payload, &payloadMap); err != nil {
		// Can't parse - return original
		return scheduled.Payload
	}

	since, ok := payloadMap["since"].(string)
	if !ok || since != "last_run" {
		return scheduled.Payload
	}

	// Resolve last_run to actual timestamp
	if scheduled.LastRunAt != nil {
		payloadMap["since"] = *scheduled.LastRunAt
		t.logger.Debugw(sym.Pulse+" Resolved --since last_run to timestamp",
			"job_id", scheduled.ID,
			"last_run_at", *scheduled.LastRunAt)
	} else {
		// No last run - remove since filter (process all)
		delete(payloadMap, "since")
		t.logger.Debugw(sym.Pulse+" No last_run_at, removing --since filter (first run)",
			"job_id", scheduled.ID)
	}

	// Re-serialize
	resolved, err := json.Marshal(payloadMap)
	if err != nil {
		return scheduled.Payload
	}
	return resolved
}

// enqueueAsyncJob creates and enqueues an async job from the scheduled job.
// This is domain-agnostic - it uses the pre-computed handler_name and payload.
// Jobs must have HandlerName and Payload set at creation time by the application.
//
// Special handling for "since":"last_run" in payload:
// If the payload contains this value, it will be resolved to the actual
// last_run_at timestamp from the scheduled job, enabling incremental processing.
func (t *Ticker) enqueueAsyncJob(scheduled *Job) (string, error) {
	// Require pre-computed handler - jobs should be created by the application
	// with handler_name and payload populated
	if scheduled.HandlerName == "" {
		return "", fmt.Errorf("scheduled job %s missing handler_name (job may need re-creation)", scheduled.ID)
	}

	handlerName := scheduled.HandlerName
	payload := t.resolvePayloadLastRun(scheduled)
	sourceURL := scheduled.SourceURL

	// Check for existing active job with same source URL (deduplication)
	existingJob, err := t.queue.FindActiveJobBySourceAndHandler(sourceURL, handlerName)
	if err != nil {
		return "", fmt.Errorf("failed to check for duplicate job: %w", err)
	}

	if existingJob != nil {
		// Job already exists and is active - return existing job ID
		t.logger.Debugw(sym.Pulse+" Skipping duplicate job",
			"source_url", sourceURL,
			"handler", handlerName,
			"existing_job_id", existingJob.ID,
			"existing_status", existingJob.Status)
		return existingJob.ID, nil
	}

	// Create async job with handler name and payload
	job, err := async.NewJobWithPayload(
		handlerName,
		sourceURL,
		payload,
		0,   // Total operations unknown
		0.0, // Cost calculated during execution
		fmt.Sprintf("pulse:%s", scheduled.ID),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create async job: %w", err)
	}

	// Enqueue the job
	if err := t.queue.Enqueue(job); err != nil {
		return "", fmt.Errorf("failed to enqueue job: %w", err)
	}

	t.logger.Debugw(sym.Pulse+" Enqueued async job",
		"source_url", sourceURL,
		"job_id", job.ID,
		"handler", handlerName,
		"scheduled_job_id", scheduled.ID)

	return job.ID, nil
}

// GetStats returns ticker statistics
func (t *Ticker) GetStats() map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	return map[string]interface{}{
		"last_tick_at":      t.lastTickAt,
		"ticks_since_start": t.ticksSinceStart,
		"interval":          t.interval,
	}
}
