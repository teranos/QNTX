# Two-Phase Job Pattern

## Overview

Pulse supports a two-phase job pattern for complex workflows that need to spawn child tasks and aggregate results.

## Job Phases

### Phase 1: Ingest
- Process initial data
- Create sub-entities
- Enqueue child tasks
- Track child job IDs

### Phase 2: Aggregate
- Wait for child tasks to complete
- Aggregate results
- Perform final processing
- Update parent entity

## Implementation

Jobs track their phase using JobMetadata:

```go
type JobMetadata struct {
    Phase string `json:"phase,omitempty"` // "ingest" or "aggregate"
    // Other metadata fields as needed
}
```

### Phase Detection

```go
if job.Metadata != nil && job.Metadata.Phase == "aggregate" {
    // Aggregate phase logic
} else {
    // Ingest phase logic (default)
}
```

### Parent-Child Relationships

Jobs maintain parent-child relationships through:
- `parent_job_id` field in the async_ix_jobs table
- Tracking child job IDs in parent's payload
- Status propagation from children to parent

### Cascade Deletion

**Location**: `pulse/async/queue.go:DeleteJobWithChildren()`

When a parent job is deleted:

1. System finds all child tasks associated with parent
2. Marks all active child tasks as `cancelled` with reason `parent job deleted`
3. Deletes the parent job from database
4. Preserves completed/failed children for audit trail

**Race condition protection**: Before enqueueing children, the parent checks it still exists in the database, which prevents enqueueing tasks after parent deletion during execution.

### Orphan Cleanup

**Location**: `pulse/async/queue.go:cancelOrphanedChildren()`

When a parent job completes or fails:

1. System finds all child tasks still active (queued/running/paused)
2. Cancels each child with reason `parent job completed`
3. Preserves completed/failed/cancelled children for history

**Behavior**:
- **Queued children**: Cancelled immediately, never execute
- **Running children**: Marked cancelled in DB, current execution completes but result ignored
- **Paused children**: Cancelled
- **Completed/failed children**: Preserved unchanged

### Retry Logic

**Location**: `pulse/async/error.go:RetryableError()`

Failed tasks can be retried automatically (max 2 retries = 3 total attempts):

1. Task fails with retryable error (AI failure, network error, timeout)
2. System increments `retry_count` and re-queues job
3. Logs retry attempt: `꩜ Retry 1/2: operation failed | job:JB_abc123`
4. After max retries exceeded, logs: `꩜ Max retries exceeded (2): operation failed | job:JB_abc123`

**Database tracking**: Each retry attempt updates the job record with retry count and error details, providing full audit trail.

## Example Workflow

```
1. Parent job starts (ingest phase)
   ↓
2. Creates N child jobs
   ↓
3. Parent pauses/waits
   ↓
4. Children complete
   ↓
5. Parent resumes (aggregate phase)
   ↓
6. Parent aggregates results
   ↓
7. Parent completes
```

## Use Cases

- **Batch Processing**: Process list of items, aggregate results
- **Hierarchical Data**: Process parent entity, then children
- **Fan-out/Fan-in**: Distribute work, collect results
- **Multi-stage Pipelines**: Sequential processing stages

## Configuration

No special configuration required. The pattern is implemented through job handler logic and JobMetadata.

## Best Practices

1. **Always set phase explicitly** in JobMetadata when using this pattern
2. **Track child job IDs** in parent payload for monitoring
3. **Handle partial failures** - some children may fail
4. **Set reasonable timeouts** for aggregate phase
5. **Use retry logic** appropriately for each phase

## Verified By

Parent-child job hierarchy:
- `TestParentJobHierarchy` - [pulse/async/job_test.go](https://github.com/teranos/QNTX/blob/main/pulse/async/job_test.go)
- `TestTASBotParentJobHierarchy` - [pulse/async/store_test.go](https://github.com/teranos/QNTX/blob/main/pulse/async/store_test.go)

## Related Documentation

- [Pulse Async Architecture](pulse-async-ix.md)
- [Opening (✿) and Closing (❀)](../development/grace.md) - Handles job recovery
- [Job Type Definitions](../types/async.md)