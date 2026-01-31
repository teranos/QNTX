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

Phase recovery during graceful restart:
- `TestGRACEPhaseRecoveryNoChildTasks` - [pulse/async/grace_test.go:492](https://github.com/teranos/QNTX/blob/main/pulse/async/grace_test.go#L492)
- `TestGRACEPhaseRecoveryWithChildTasks` - [pulse/async/grace_test.go:542](https://github.com/teranos/QNTX/blob/main/pulse/async/grace_test.go#L542)

Parent-child job hierarchy:
- `TestParentJobHierarchy` - [pulse/async/job_test.go:369](https://github.com/teranos/QNTX/blob/main/pulse/async/job_test.go#L369)
- `TestTASBotParentJobHierarchy` - [pulse/async/store_test.go:250](https://github.com/teranos/QNTX/blob/main/pulse/async/store_test.go#L250)

## Related Documentation

- [Pulse Async Architecture](pulse-async-ix.md)
- [Opening (✿) and Closing (❀)](../development/grace.md) - Handles job recovery
- [Job Type Definitions](../types/async.md)