# Pulse & Async IX: Budget-Controlled Asynchronous Job Processing

## Overview

**Pulse** is QNTX's rate-limiting and budget control system for asynchronous operations. It enables long-running, cost-sensitive operations to run asynchronously while adhering to API rate limits and money budgets.

## Motivation

Modern AI-powered applications often involve:
- **Multiple API calls** per operation (batch processing)
- **Real money costs** (~$0.002+ per API call)
- **Time-intensive operations** (10-30+ seconds)
- **Batch processing needs** (multiple items, re-processing)

Without controls:
- ❌ Blocking operations during processing (poor UX)
- ❌ Uncontrolled API costs (budget overrun)
- ❌ Rate limit violations (429 errors)
- ❌ No visibility into running operations
- ❌ Can't pause/stop expensive operations

## Architecture

### Pulse System

Pulse acts as a **smart rate limiter and budget manager** for outgoing API calls.

```
┌─────────────────────────────────────────┐
│          Pulse Controller               │
├─────────────────────────────────────────┤
│  ├─ Rate Limiter (calls/minute)         │
│  ├─ Budget Tracker (daily/monthly USD)  │
│  ├─ Cost Estimator (per operation)      │
│  ├─ Pause/Resume Control                │
│  └─ Observability Integration           │
└─────────────────────────────────────────┘
              ↓  ↑
       ┌──────────────┐
       │ Async Job    │
       └──────────────┘
```

**Key Responsibilities:**
- **Rate limiting**: Enforce max API calls per minute (sliding window algorithm)
- **Budget tracking**: Monitor daily/monthly spend in USD
- **Cost estimation**: Calculate projected costs before operations
- **Pause control**: Stop operations when budget exceeded
- **Observability**: Integrate with usage tracker

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
    CostEstimate float64         // Estimated USD cost
    CostActual   float64         // Actual USD cost so far
    PulseState   *PulseState     // Rate limit, budget status
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
    SpendToday       float64
    SpendThisMonth   float64
    BudgetRemaining  float64
    IsPaused         bool
    PauseReason      string  // "budget_exceeded", "rate_limit", "user_requested"
}

// Handler-based execution
type JobHandler interface {
    Name() string                                    // "domain.operation"
    Execute(ctx context.Context, job *Job) error
}
```

**Generic Architecture:**
- No JobType enum - handlers identified by string name
- No JobMetadata struct - payloads are handler-specific JSON
- Domain packages define their own payload types
- Infrastructure (async package) is fully domain-agnostic

### Configuration

Example configuration:

```toml
[pulse]
max_calls_per_minute = 10
daily_budget_usd = 5.0
monthly_budget_usd = 100.0
pause_on_budget_exceeded = true
```

## Implementation

### Database Schema

#### Pulse Budget Tracking

```sql
CREATE TABLE pulse_budget (
    date TEXT PRIMARY KEY,           -- "2025-11-23" for daily, "2025-11" for monthly
    type TEXT NOT NULL,              -- "daily" or "monthly"
    spend_usd REAL NOT NULL,         -- Current spend in USD
    operations_count INTEGER NOT NULL,
    created_at DATETIME,
    updated_at DATETIME
);

CREATE INDEX idx_pulse_budget_type ON pulse_budget(type);
```

#### Async Job Queue

```sql
CREATE TABLE async_ix_jobs (
    id TEXT PRIMARY KEY,             -- Job ID (ASID)
    handler_name TEXT,               -- Handler identifier
    source TEXT NOT NULL,            -- Data source (for deduplication)
    status TEXT NOT NULL,            -- "queued", "running", "paused", "completed", "failed"
    progress_current INTEGER,        -- Current operations completed
    progress_total INTEGER,          -- Total operations
    cost_estimate REAL,              -- Estimated USD cost
    cost_actual REAL,                -- Actual USD cost
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

#### Rate Limiter (`internal/pulse/budget/limiter.go`)

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

#### Budget Tracker (`internal/pulse/budget/tracker.go`)

Tracks daily/monthly spend with persistence:

```go
type Tracker struct {
    store  *Store
    config BudgetConfig
    mu     sync.RWMutex
}

func (b *Tracker) CheckBudget(estimatedCost float64) error {
    // Check if operation would exceed budget
}

func (b *Tracker) RecordOperation(actualCost float64) error {
    // Record actual cost in database
}

func (b *Tracker) GetStatus() (*Status, error) {
    // Returns current budget status from ai_model_usage table
}
```

**Package:** `internal/pulse/budget` - Separated from async to eliminate import cycles

#### Job Queue (`internal/pulse/async/queue.go`)

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

#### Worker Pool (`internal/pulse/async/worker.go`)

Processes jobs with pulse integration:

```go
type WorkerPool struct {
    queue         *Queue
    budgetTracker *budget.Tracker  // Optional - can be nil for tests
    rateLimiter   *budget.Limiter  // Optional - can be nil for tests
    workers       int
    executor      JobExecutor
}

// Start begins processing jobs
func (wp *WorkerPool) Start()

// Stop gracefully stops workers
func (wp *WorkerPool) Stop()

// processNextJob processes one job with rate limiting and budget checks
func (wp *WorkerPool) processNextJob() error {
    // 1. Dequeue job
    // 2. Check rate limit (pause if exceeded)
    // 3. Check budget (pause if exceeded)
    // 4. Execute job via handler registry
    // 5. Mark complete/failed
}
```

**Worker Pool Features:**
- Configurable worker count
- Gradual startup (1s → 5s polling interval)
- Graceful shutdown with 2s timeout
- Rate limiting before budget checks
- Job pause/resume on limit violations

## Implementation Status

### Phase 1: Pulse Foundation ✅ COMPLETE

- ✅ Configuration system
- ✅ Budget tracking (pulse_budget table)
- ✅ Budget checking before operations
- ✅ Cost recording after operations
- ✅ Database migrations
- ✅ **Refactored (Dec 2025)**: Separated budget/rate limiting into `internal/pulse/budget` package

**Files:**
- `internal/pulse/budget/limiter.go` - Rate limiter
- `internal/pulse/budget/tracker.go` - Budget tracker
- `internal/pulse/budget/store.go` - Budget persistence
- `internal/pulse/store.go` - Scheduled jobs persistence

**Architecture Benefits:**
- Clean separation of concerns (budget tracking vs job execution)
- Eliminates import cycles
- Budget/rate limiting reusable across packages
- Simpler testing (budget and async tested independently)

### Phase 2: Async Job System ✅ COMPLETE

- ✅ Async job queue (async_ix_jobs table)
- ✅ Job models and state management
- ✅ Job store (CRUD operations)
- ✅ Queue with pub/sub
- ✅ Worker pool with pulse integration
- ✅ Rate limiting enforcement
- ✅ Unit tests (41/41 passing)
- ✅ **Refactored (Dec 2025)**: Generic handler-based architecture

**Files:**
- `internal/pulse/async/job.go` - Generic job model (handler-based)
- `internal/pulse/async/handler.go` - JobHandler interface and registry
- `internal/pulse/async/store.go` - Job persistence
- `internal/pulse/async/queue.go` - Queue operations
- `internal/pulse/async/worker.go` - Worker pool with budget integration

## Testing Strategy

### Unit Tests

- `pulse/budget/limiter_test.go` - Rate limiting (9/9 tests)
- `pulse/budget/tracker_test.go` - Budget calculations
- `pulse/async/job_test.go` - Job models and state
- `pulse/async/queue_test.go` - Queue operations
- `pulse/async/store_test.go` - Persistence
- `pulse/async/worker_test.go` - Worker pool and integration

**Total: 41/41 tests passing**

### Integration Tests

Full async workflow end-to-end:
- Job enqueueing and dequeuing
- Budget exceeded scenarios
- Rate limiting enforcement
- Pause/resume functionality
- Worker pool lifecycle

## Future Enhancements

### Dynamic Cost Estimation

Query pricing APIs for real-time cost updates:
- Automatic pricing updates when providers change rates
- Support for multiple models with different pricing
- Config override vs API pricing

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
- Smart fallback when budget low
- Budget-aware model selection

### Cost Optimization

Reduce API costs through intelligent caching:
- Cache results for similar inputs
- Similarity detection
- Time-based cache expiration
- Batch API calls (when supported)

## Related Documentation

- **Handler Implementation**: Applications define domain-specific handlers implementing the JobHandler interface
- **Configuration**: Applications configure pulse settings in their config.toml
- **Integration Examples**: See application-specific documentation for integration patterns
