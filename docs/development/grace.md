# Opening (✿) and Closing (❀)

Graceful shutdown and startup system for async job processing.

**Symbols:**
- **✿ Opening** - Graceful startup with orphaned job recovery
- **❀ Closing** - Graceful shutdown with checkpoint preservation

_(Formerly codename: GRACE - Graceful Async Cancellation Engine)_

## Implementation Summary

### ❀ Closing (Graceful Shutdown)
- **Context propagation**: Application → Worker Pool → Jobs → Handlers
- **Plugin shutdown**: Plugins receive shutdown signal via gRPC, complete in-flight work
- **Task-level atomicity**: Jobs complete current task before checkpointing
- **Signal handling**: Application catches signals, triggers shutdown
- **Worker timeout**: 30 seconds for clean checkpoint and exit
- **Job re-queuing**: Cancelled jobs transition to `queued` status with checkpoint intact

### ✿ Opening (Graceful Start)
- **Orphan detection**: Finds jobs stuck in `running` state after crash
- **Super gradual warm start**:
  - Immediate: First job only
  - Warm start (0-10s): Jobs 2-10 at 1 job/second
  - Slow start (10s-15min): Remaining jobs spread evenly
- **Checkpoint priority**: Jobs with checkpoints recovered first
- **Configurable timing**: Test mode uses faster intervals (10s phases)

### Key Files
- `pulse/async/worker.go` - Graceful start/stop logic
- `pulse/async/grace_test.go` - Test suite
- Handler implementations - Task-level context checks

### Testing

**Verified by** (`pulse/async/grace_test.go`):
- `TestGRACEShutdownFlow` (:25) - Context propagation and clean exit
- `TestGRACECheckpointSaving` (:145) - Progress preserved on cancel
- `TestGRACEWorkerShutdownTimeout` (:183) - Timeout enforcement
- `TestGRACEGracefulStart` (:228) - Orphan detection and recovery
- `TestGRACEGradualRecovery` (:349) - Super gradual warm start pacing

```bash
# Fast tests (~10s)
go test ./pulse/async -run TestGRACE -short

# Full integration tests (~60s)
go test ./pulse/async -run TestGRACE
```

## Phase Recovery Architecture

### Two-Phase Job Pattern

Some jobs use a two-phase execution pattern:

1. **"ingest" phase**: Process data, create sub-entities, enqueue child tasks
2. **"aggregate" phase**: Wait for child tasks to complete, aggregate results

This pattern solves parent-child job coordination without blocking worker threads.

### Smart Phase Recovery (Implemented)

**Location**: `pulse/async/worker.go:requeueOrphanedJob()`

When recovering orphaned jobs, GRACE validates phase consistency:

```go
// Example: Check if child tasks actually exist for aggregate phase
if job.Metadata != nil && job.Metadata.Phase == "aggregate" {
    tasks, err := wp.queue.ListTasksByParent(job.ID)
    if err != nil || len(tasks) == 0 {
        // No tasks found - reset to ingest phase
        job.Metadata.Phase = ""
        log.Printf("GRACE: Reset job %s to 'ingest' phase (no child tasks found)")
    } else {
        // Tasks exist - keep aggregate phase
        log.Printf("GRACE: Job %s staying in 'aggregate' phase (%d child tasks found)")
    }
}
```

**Scenarios handled**:

1. **Crash during phase transition**:
   - Job set phase="aggregate" but crash before creating tasks
   - Recovery: Reset phase to "" (defaults to "ingest")
   - Job re-runs from beginning, creates tasks properly

2. **Normal crash after task creation**:
   - Job in phase="aggregate" with tasks already created
   - Recovery: Keep phase="aggregate"
   - Job continues aggregating task results

3. **Crash during ingest phase**:
   - Job in phase="" or phase="ingest"
   - Recovery: No special handling needed
   - Job re-runs from beginning

### Testing

**Verified by** (`pulse/async/grace_test.go`):
- `TestGRACEPhaseRecoveryNoChildTasks` (:492) - Reset when no tasks exist
- `TestGRACEPhaseRecoveryWithChildTasks` (:542) - Preserve when tasks exist

```bash
go test ./pulse/async -run TestGRACEPhaseRecovery -v
```

### Implementation Details

**Error handling**: If checking tasks fails (DB error), defaults to safe behavior (reset phase)

**Logging**: All phase decisions logged for debugging:
- "Reset job X to 'ingest' phase (no child tasks found, likely crashed during phase transition)"
- "Job X staying in 'aggregate' phase (N child tasks found)"
- "Reset job X to 'ingest' phase (couldn't verify child tasks)"

**Backward compatibility**: Only affects jobs with two-phase metadata; other job types unaffected

## Parent-Child Job Lifecycle

### Overview

Parent jobs spawn child tasks (subtasks). The system ensures child tasks are properly managed throughout the parent's lifecycle.

### Cascade Deletion

**Location**: `pulse/async/queue.go:DeleteJobWithChildren()`

When a parent job is deleted:

1. System finds all child tasks associated with parent
2. Marks all active child tasks as `cancelled` with reason "parent job deleted"
3. Deletes the parent job from database
4. Preserves completed/failed children for audit trail

**Race condition protection**: Before enqueueing children, parent checks if it still exists in database. This prevents enqueueing tasks after parent deletion during execution.

```go
// Check if parent job still exists before enqueueing children
if _, err := queue.GetJob(job.ID); err != nil {
    return fmt.Errorf("parent job deleted during execution: %w", err)
}
```

### Orphan Cleanup

**Location**: `pulse/async/queue.go:cancelOrphanedChildren()`

When a parent job completes or fails:

1. System finds all child tasks still active (queued/running/paused)
2. Cancels each child with reason "parent job completed"
3. Preserves completed/failed/cancelled children for history

**Behavior**:
- **Queued children**: Cancelled immediately, never execute
- **Running children**: Marked cancelled in DB, current execution completes but result ignored
- **Paused children**: Cancelled
- **Completed/failed children**: Preserved unchanged

### Retry Logic

**Location**: `pulse/async/error.go:RetryableError()`

Failed tasks can be retried automatically (max 3 attempts total):

1. Task fails with retryable error (AI failure, network error, timeout)
2. System increments `retry_count` and re-queues job
3. Logs retry attempt: `꩜ Retry 1/2: operation failed | job:JB_abc123`
4. After max retries, logs: `꩜ Max retries exceeded (2): operation failed | job:JB_abc123`

**Database tracking**: Each retry attempt updates the job record with retry count and error details, providing full audit trail.

### Testing

**Verified by:**
- `TestParentJobHierarchy` - `pulse/async/job_test.go:369`
- `TestTASBotParentJobHierarchy` - `pulse/async/store_test.go:250`

```bash
go test ./pulse/async -run TestParentJobHierarchy -v
```

## Integration Guide

### Application Shutdown

Applications using Pulse should propagate shutdown signals:

```go
// Create worker pool with application context
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

workerPool := async.NewWorkerPool(ctx, db, queue, executor, config)
workerPool.Start()

// Handle shutdown signals
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

<-sigChan
log.Println("Shutdown signal received, stopping workers...")
cancel() // Triggers graceful shutdown

// Wait for workers to finish (with timeout)
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer shutdownCancel()
workerPool.StopWithContext(shutdownCtx)
```

### Handler Context Checks

Job handlers should check context at task boundaries:

```go
func (h *MyHandler) Execute(ctx context.Context, job *async.Job) error {
    for _, item := range items {
        // Check for cancellation before each task
        select {
        case <-ctx.Done():
            return ctx.Err() // Job will be checkpointed
        default:
        }

        // Process item
        if err := processItem(ctx, item); err != nil {
            return err
        }

        // Update progress
        job.Progress.Current++
    }

    return nil
}
```

## Configuration

### Worker Pool Config

```go
type WorkerPoolConfig struct {
    Workers       int           // Number of concurrent workers
    PollInterval  time.Duration // How often to check for jobs
    ShutdownTimeout time.Duration // Max time to wait for graceful shutdown
}
```

### Test Mode

For faster testing, use shorter intervals:

```go
config := async.WorkerPoolConfig{
    Workers:         1,
    PollInterval:    100 * time.Millisecond,
    ShutdownTimeout: 2 * time.Second,
}
```

---
**Status**: Implemented and tested
