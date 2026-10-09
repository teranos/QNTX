# Pulse & Async IX: Asynchronous Job Processing

## Overview

**Pulse** is QNTX's asynchronous job system. It enables long-running operations to run asynchronously while adhering to API rate limits. For the HTTP API that controls Pulse, see the [Pulse API Documentation](https://github.com/teranos/QNTX/blob/main/server/openapi/openapi.json).

## Motivation

Modern AI-powered applications often involve:
- **Multiple API calls** per operation (batch processing)
- **Time-intensive operations** (10-30+ seconds)
- **Batch processing needs** (multiple items, re-processing)

Without controls:
- ❌ Blocking operations during processing (poor UX)
- ❌ Rate limit violations (429 errors)
- ❌ No visibility into running operations
- ❌ Can't pause/stop expensive operations

## Architecture

### Pulse System

Pulse acts as a **rate limiter** for outgoing API calls.

```
┌─────────────────────────────────────────┐
│          Pulse Controller               │
├─────────────────────────────────────────┤
│  ├─ Rate Limiter (calls/minute)         │
│  └─ Pause/Resume Control                │
└─────────────────────────────────────────┘
              ↓  ↑
       ┌──────────────┐
       │ Async Job    │
       └──────────────┘
```

**Key Responsibilities:**
- **Rate limiting**: Enforce max API calls per minute (sliding window algorithm)
- **Pause control**: Pause a job when the rate limit is reached

### Async Job System

Jobs run asynchronously with pulse control using a generic handler-based architecture.

```go
// Generic Async Job (handler-based architecture)
type Job struct {
    ID           string          // Unique job ID (ASID)
    HandlerName  string          // Handler identifier (e.g., "domain.operation")
    Payload      json.RawMessage // Handler-specific data (domain-owned)
    Source       string          // Data source (for deduplication)
    Status       JobStatus       // "queued", "running", "paused", "completed", "failed"
    Progress     Progress        // Current/total operations
    PulseState   *PulseState     // Rate limit status
    Error        string          // Error message if failed
    ParentJobID  string          // For task hierarchies
    RetryCount   int             // Retry attempts (max 2)
    CreatedAt    time.Time
    StartedAt    *time.Time
    CompletedAt  *time.Time
    UpdatedAt    time.Time
}

type PulseState struct {
    CallsThisMinute  int
    CallsRemaining   int
    IsPaused         bool
    PauseReason      string  // "rate_limited", "user_requested"
}

// Handler-based execution
type JobHandler interface {
    Name() string                                    // "domain.operation"
    Execute(ctx context.Context, job *Job) error
}
```

> **Type Reference**: See [Job](../types/async.md#job), [PulseState](../types/async.md#pulsestate), and [Progress](../types/async.md#progress) type definitions.

**Generic Architecture:**
- No JobType enum - handlers identified by string name
- JobMetadata exists for phase tracking in two-phase jobs (ingest/aggregate patterns)
- Domain packages define their own payload types
- Infrastructure (async package) is domain-agnostic with minimal job coordination support

### Configuration

Example Pulse configuration (see [config package](../../internal/config/README.md) for full config system):

```toml
[pulse]
workers = 1
ticker_interval_seconds = 1
```

## Implementation

### Database Schema

#### Async Job Queue

```sql
CREATE TABLE async_ix_jobs (
    id TEXT PRIMARY KEY,             -- Job ID (ASID)
    handler_name TEXT,               -- Handler identifier
    source TEXT NOT NULL,            -- Data source (for deduplication)
    status TEXT NOT NULL,            -- "queued", "running", "paused", "completed", "failed"
    progress_current INTEGER,        -- Current operations completed
    progress_total INTEGER,          -- Total operations
    cost_estimate REAL,              -- No longer written
    cost_actual REAL,                -- No longer written
    pulse_state TEXT,                -- JSON: PulseState
    error TEXT,                      -- Error message if failed
    payload TEXT,                    -- JSON: Handler-specific data
    parent_job_id TEXT,              -- Parent job for task hierarchies
    retry_count INTEGER DEFAULT 0,   -- Retry attempts (max 2)
    created_at DATETIME,
    started_at DATETIME,
    completed_at DATETIME,
    updated_at DATETIME
);

CREATE INDEX idx_async_ix_jobs_status ON async_ix_jobs(status);
CREATE INDEX idx_async_ix_jobs_created ON async_ix_jobs(created_at DESC);
CREATE INDEX idx_async_ix_jobs_handler ON async_ix_jobs(handler_name);
CREATE INDEX idx_async_ix_jobs_source_handler ON async_ix_jobs(source, handler_name);
```

### Core Components

#### Rate Limiter (`pulse/ratelimit/limiter.go`)

Sliding window rate limiter with configurable calls per minute:

```go
type Limiter struct {
    maxCallsPerMinute int
    window            time.Duration
    mu                sync.Mutex
    callTimes         []time.Time
}

func (r *Limiter) Allow() error {
    // Check if call allowed within rate limit
    // Returns error if rate limit exceeded
}

func (r *Limiter) Wait(ctx context.Context) error {
    // Blocks until call is allowed or context cancelled
}
```

**Features:**
- Thread-safe concurrent access
- Sliding 60-second window
- Automatic expiration of old calls
- Stats tracking (calls in window, remaining)
- Context-aware blocking with Wait()

#### Job Queue (`pulse/async/queue.go`)

Manages async job lifecycle:

```go
type Queue struct {
    store *Store
}

// Enqueue adds job to queue
func (q *Queue) Enqueue(job *Job) error

// Dequeue gets next runnable job (queued or scheduled, not paused)
func (q *Queue) Dequeue() (*Job, error)

// PauseJob pauses a running job
func (q *Queue) PauseJob(jobID string, reason string) error

// ResumeJob resumes a paused job
func (q *Queue) ResumeJob(jobID string) error

// CompleteJob marks job as completed
func (q *Queue) CompleteJob(jobID string) error

// FailJob marks job as failed with error
func (q *Queue) FailJob(jobID string, err error) error
```

#### Worker Pool (`pulse/async/worker.go`)

Processes jobs with pulse integration:

```go
type WorkerPool struct {
    queue         *Queue
    rateLimiter   RateLimiter      // Optional - can be nil for tests
    workers       int
    executor      JobExecutor
}

// Start begins processing jobs
func (wp *WorkerPool) Start()

// Stop gracefully stops workers
func (wp *WorkerPool) Stop()

// processNextJob processes one job with rate limiting
func (wp *WorkerPool) processNextJob() error {
    // 1. Dequeue job
    // 2. Check rate limit (pause if exceeded)
    // 3. Execute job via handler registry
    // 4. Mark complete/failed
}
```

**Worker Pool Features:**
- Configurable worker count
- Gradual startup (1s → 5s polling interval)
- Graceful shutdown with 2s timeout
- Job pause/resume on limit violations

### Handler Registration

**Current Limitation**: Handler availability validation happens at job execution time, not at job creation time. This can lead to jobs being created successfully but failing when executed because the required handler is not registered.

**Example Scenario**:
```
1. User creates IX job: "ix https://github.com/user/repo"
2. ATS parser (server/ats_parser.go) hardcodes HandlerName: "ixgest.git"
3. Job is created successfully in scheduled_pulse_jobs table
4. Job is executed by ticker and enqueued to async queue
5. Worker pool tries to execute job
6. Handler lookup fails: "no handler registered for handler name: ixgest.git"
7. Job fails with error, pulse_execution record updated to 'failed'
```

**Why This Happens**:
- Plugin-provided handlers (e.g., from qntx-code plugin) cannot register with server's HandlerRegistry
- No bridge exists between plugin initialization and server's handler registry
- ATS parser runs in server package, has no visibility into plugin state
- Handler validation only happens in worker pool during execution

**Current Behavior**:
- Jobs created successfully regardless of handler availability
- Error feedback provided through pulse_execution table and WebSocket events
- IX elements show red background with "no handler registered" error message
- Users see full error context in UI (Issue #356)

**Future Improvements** (separate issue):

1. **Early validation**: Check handler availability during job creation
   - Requires handler registry accessible to ATS parser
   - May need plugin-to-server handler registration bridge

2. **Plugin-aware parsing**: ATS parser queries plugin registry before hardcoding handler names
   - Requires dependency on plugin system (architectural trade-off)
   - May violate domain separation principles

3. **Graceful degradation**: Allow jobs to be created but mark as "pending handler availability"
   - Job remains in special state until plugin loads
   - Auto-resume when handler becomes available

**References**:
- `TODO(plugin-pulse-integration)` in `server/ats_parser.go`
- Issue #356: Wire IX elements to Pulse execution
- PR #357: Add visual status feedback to IX elements

## Implementation Status

### Phase 1: Pulse Foundation ✅ COMPLETE

- ✅ Configuration system
- ✅ Database migrations

**Files:**
- `pulse/ratelimit/limiter.go` - Rate limiter

### Phase 2: Async Job System ✅ COMPLETE

- ✅ Async job queue (async_ix_jobs table)
- ✅ Job models and state management
- ✅ Job store (CRUD operations)
- ✅ Queue with pub/sub
- ✅ Worker pool with pulse integration
- ✅ Rate limiting enforcement
- ✅ Unit tests (41/41 passing)
- ✅ **Refactored (Dec 2025)**: Generic handler-based architecture
- ✅ **Opening (✿) and Closing (❀) (Dec 2025)**: Graceful startup/shutdown with orphan recovery

**Files:**
- `pulse/async/job.go` - Generic job model (handler-based)
- `pulse/async/handler.go` - JobHandler interface and registry
- `pulse/async/store.go` - Job persistence
- `pulse/async/queue.go` - Queue operations
- `pulse/async/worker.go` - Worker pool
- `pulse/async/grace_test.go` - Opening/Closing tests

## Testing Strategy

### Unit Tests

- `pulse/ratelimit/limiter_test.go` - Rate limiting
- `pulse/async/job_test.go` - Job models and state
- `pulse/async/queue_test.go` - Queue operations
- `pulse/async/store_test.go` - Persistence
- `pulse/async/worker_test.go` - Worker pool and integration

**Total: 41/41 tests passing**

### Integration Tests

Full async workflow end-to-end:
- Job enqueueing and dequeuing
- Rate limiting enforcement
- Pause/resume functionality
- Worker pool lifecycle
- Graceful shutdown ❀ and orphan recovery ✿

## Future Enhancements

### Priority Queues

Allow high-priority jobs to skip the queue:
- High/normal/low priority levels
- Fair scheduling to prevent starvation
- Priority flags in job creation

### Scheduling

Schedule expensive operations for specific times:
- Specific time scheduling
- Cron expressions
- Timezone handling

### Multi-Model Support

Configure different models for different operations:
- Fast/cheap model for screening
- Better model for detailed processing

### Cost Optimization

Reduce API costs through intelligent caching:
- Cache results for similar inputs
- Similarity detection
- Time-based cache expiration
- Batch API calls (when supported)

## Use Case Examples

### Example 1: Batch Data Processing

```go
// Define payload type for your domain
type BatchProcessPayload struct {
    SourceURL string   `json:"source_url"`
    RecordIDs []string `json:"record_ids"`
    BatchType string   `json:"batch_type"`
}

// Implement handler
type BatchProcessHandler struct {
    dataService DataService
    queue       *async.Queue
    logger      *zap.Logger
}

func (h *BatchProcessHandler) Name() string {
    return "data.batch-process"
}

func (h *BatchProcessHandler) Execute(ctx context.Context, job *async.Job) error {
    var payload BatchProcessPayload
    if err := json.Unmarshal(job.Payload, &payload); err != nil {
        return fmt.Errorf("invalid payload: %w", err)
    }

    // Process with progress tracking
    for i, recordID := range payload.RecordIDs {
        // Check for cancellation
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // Process record
        if err := h.dataService.ProcessRecord(ctx, recordID); err != nil {
            return fmt.Errorf("failed to process record %s: %w", recordID, err)
        }

        // Update progress
        job.Progress.Current = i + 1
        if err := h.queue.UpdateJob(job); err != nil {
            h.logger.Warn("Failed to update job progress", zap.Error(err))
        }
    }

    return nil
}

// Register handler
registry := async.NewHandlerRegistry()
registry.Register(&BatchProcessHandler{
    dataService: myDataService,
    queue:       queue,
    logger:      logger,
})
```

### Example 2: ML Model Inference

```go
type InferencePayload struct {
    ModelName  string   `json:"model_name"`
    InputData  []string `json:"input_data"`
    BatchSize  int      `json:"batch_size"`
}

type InferenceHandler struct {
    mlService MLService
    queue     *async.Queue
}

func (h *InferenceHandler) Name() string {
    return "ml.inference"
}

func (h *InferenceHandler) Execute(ctx context.Context, job *async.Job) error {
    var payload InferencePayload
    if err := json.Unmarshal(job.Payload, &payload); err != nil {
        return fmt.Errorf("invalid payload: %w", err)
    }

    results, err := h.mlService.RunInference(ctx, payload.ModelName, payload.InputData)
    if err != nil {
        return err
    }

    return h.queue.UpdateJob(job)
}
```

## Related Documentation

- **GRACE (❀)**: [ADR-036-GRACE.md](../adr/ADR-036-GRACE.md) - Graceful shutdown
- **Handler Implementation**: Applications define domain-specific handlers implementing the JobHandler interface
- **Configuration**: [config-system.md](config-system.md) - Configuration system including Pulse settings
- **Resource Coordination**: [pulse-resource-coordination.md](pulse-resource-coordination.md) - GPU and system resource management
