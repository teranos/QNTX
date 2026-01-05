# Batch Attestation API Design

## Overview

This document proposes a batch attestation API for the plugin service interface, enabling plugins to create multiple attestations efficiently in a single operation. This is particularly valuable when tracking bulk operations (e.g., git ingestion processing hundreds of commits).

## Current State

### Existing BatchStore Interface

QNTX already has a `BatchStore` interface in `ats/store.go`:

```go
type BatchStore interface {
    PersistItems(items []AttestationItem, sourcePrefix string) *PersistenceResult
}
```

**Key Features:**
- Converts `AttestationItem` instances to attestations in batch
- Returns detailed `PersistenceResult` with success/failure metrics
- Used by git ingestion for efficient commit processing
- Transaction-based for performance

**Limitation:** `AttestationItem` is tied to the ingestion subsystem. Plugins need a more general-purpose batch API.

### Current Plugin Pattern

Plugins currently create attestations one at a time:

```go
// Called multiple times in a loop
cmd := &types.AsCommand{
    Subjects:   []string{subject},
    Predicates: []string{predicate},
    Contexts:   []string{context},
}
_, err := store.GenerateAndCreateAttestation(cmd)
```

**Issues:**
- Multiple database transactions
- No atomic batch operations
- No aggregate error handling
- Performance overhead for bulk operations

## Proposed Solution

### 1. Add Batch Method to AttestationStore Interface

Extend the `ats.AttestationStore` interface:

```go
type AttestationStore interface {
    // Existing methods...
    CreateAttestation(as *types.As) error
    GenerateAndCreateAttestation(cmd *types.AsCommand) (*types.As, error)

    // NEW: Batch attestation creation
    GenerateAndCreateAttestations(cmds []*types.AsCommand) *BatchAttestationResult
}
```

### 2. BatchAttestationResult Type

New result type in `ats/store.go`:

```go
// BatchAttestationResult contains results from batch attestation creation
type BatchAttestationResult struct {
    // Successfully created attestations
    Attestations []*types.As

    // Number of successful creations
    SuccessCount int

    // Number of failed creations
    FailureCount int

    // Error messages for failures (indexed by original command position)
    Errors map[int]error

    // Success rate (0.0-100.0)
    SuccessRate float64
}
```

### 3. Implementation in SQLStore

Add to `ats/storage/sql.go`:

```go
func (s *SQLStore) GenerateAndCreateAttestations(cmds []*types.AsCommand) *ats.BatchAttestationResult {
    result := &ats.BatchAttestationResult{
        Attestations: make([]*types.As, 0, len(cmds)),
        Errors:       make(map[int]error),
    }

    // Begin transaction for atomic batch operation
    tx, err := s.db.Begin()
    if err != nil {
        // All failed
        result.FailureCount = len(cmds)
        for i := range cmds {
            result.Errors[i] = fmt.Errorf("failed to begin transaction: %w", err)
        }
        return result
    }
    defer tx.Rollback()

    // Process each command
    for i, cmd := range cmds {
        attestation, err := s.generateAndCreate(tx, cmd)
        if err != nil {
            result.FailureCount++
            result.Errors[i] = err
            continue
        }

        result.Attestations = append(result.Attestations, attestation)
        result.SuccessCount++
    }

    // Commit transaction
    if err := tx.Commit(); err != nil {
        // Transaction failed - all operations rolled back
        result.FailureCount = len(cmds)
        result.SuccessCount = 0
        result.Attestations = nil
        for i := range cmds {
            result.Errors[i] = fmt.Errorf("transaction commit failed: %w", err)
        }
        return result
    }

    // Calculate success rate
    if len(cmds) > 0 {
        result.SuccessRate = float64(result.SuccessCount) / float64(len(cmds)) * 100.0
    }

    return result
}
```

## Use Cases

### 1. Git Ingestion Completion

Currently creates attestations one-by-one. With batch API:

```go
func (p *Plugin) attestIxgestCompletedBatch(repoPath string, commits []CommitInfo) {
    store := p.services.ATSStore()
    if store == nil {
        return
    }

    // Create batch of attestation commands
    cmds := make([]*types.AsCommand, len(commits))
    for i, commit := range commits {
        cmds[i] = &types.AsCommand{
            Subjects:   []string{commit.SHA},
            Predicates: []string{"ingested"},
            Contexts:   []string{"ixgest-git"},
            Attributes: map[string]interface{}{
                "repo":    repoPath,
                "author":  commit.Author,
                "message": commit.Message,
            },
        }
    }

    // Single batch operation
    result := store.GenerateAndCreateAttestations(cmds)

    logger := p.services.Logger("code")
    logger.Infow("Batch ingestion attestations created",
        "success", result.SuccessCount,
        "failed", result.FailureCount,
        "rate", result.SuccessRate)
}
```

### 2. Bulk File Operations

Track multiple file reads/writes in a single operation:

```go
func (p *Plugin) attestBulkFileAccess(files []string, operation string) {
    store := p.services.ATSStore()
    if store == nil {
        return
    }

    cmds := make([]*types.AsCommand, len(files))
    for i, file := range files {
        cmds[i] = &types.AsCommand{
            Subjects:   []string{file},
            Predicates: []string{operation},
            Contexts:   []string{"code-domain"},
        }
    }

    result := store.GenerateAndCreateAttestations(cmds)
    // Handle result...
}
```

### 3. PR Review Actions

Track multiple PR interactions atomically:

```go
func (p *Plugin) attestPRReview(prNumber int, actions []ReviewAction) {
    store := p.services.ATSStore()
    if store == nil {
        return
    }

    cmds := make([]*types.AsCommand, len(actions))
    for i, action := range actions {
        cmds[i] = &types.AsCommand{
            Subjects:   []string{fmt.Sprintf("pr-%d", prNumber)},
            Predicates: []string{action.Type},
            Contexts:   []string{"github"},
            Attributes: map[string]interface{}{
                "file":    action.File,
                "line":    action.Line,
                "comment": action.Comment,
            },
        }
    }

    result := store.GenerateAndCreateAttestations(cmds)
    // Handle result...
}
```

## Benefits

### Performance
- **Single transaction**: All attestations in one database transaction
- **Reduced overhead**: One network call (gRPC) instead of N calls for external plugins
- **Better throughput**: Batch INSERT operations are faster than individual INSERTs

### Reliability
- **Atomic operations**: All succeed or all fail (transaction-based)
- **Aggregate error handling**: Single result object with all errors
- **Partial success tracking**: Know exactly which attestations succeeded/failed

### Developer Experience
- **Simpler code**: No need to manage loops with error handling
- **Better logging**: Single log entry for batch operations
- **Clearer intent**: Batch API makes bulk operations explicit

## Implementation Plan

### Phase 1: Core Implementation
1. Add `GenerateAndCreateAttestations` to `ats.AttestationStore` interface
2. Implement in `ats/storage/sql.go` (SQLStore)
3. Add `BatchAttestationResult` type to `ats/store.go`
4. Write comprehensive tests in `ats/storage/batch_test.go`

### Phase 2: Plugin Service Integration
1. Add method to plugin gRPC service definition
2. Implement gRPC handler for batch attestation creation
3. Update `RemoteServiceRegistry` to support batch calls
4. Add integration tests

### Phase 3: Plugin Adoption
1. Update code plugin to use batch API for git ingestion
2. Add batch methods to plugin attestation helpers
3. Document batch API usage patterns
4. Update CONTEXTS.md with batch examples

## Considerations

### Transaction Size Limits

**Issue**: SQLite has limits on transaction size and complexity.

**Solution**:
- Document recommended batch size (e.g., 1000 attestations per batch)
- Implement automatic chunking for very large batches
- Provide helper function: `ChunkedGenerateAndCreate(cmds, chunkSize)`

### Error Handling Strategy

**Options:**
1. **All-or-nothing** (current proposal): Transaction rolls back on any error
2. **Best-effort**: Continue processing even if some fail, commit successes
3. **Configurable**: Let caller choose behavior

**Recommendation**: Start with all-or-nothing for simplicity and consistency. Add best-effort mode if needed later.

### Backward Compatibility

The batch API is additive - existing single-attestation methods remain unchanged. No breaking changes.

### gRPC Performance

For external plugins, batch API significantly reduces gRPC overhead:
- 100 attestations: 100 gRPC calls → 1 gRPC call
- Reduces network latency by ~99% for bulk operations

## Testing Strategy

### Unit Tests
- Empty batch
- Single attestation
- Multiple attestations (success)
- Partial failures
- Complete failure
- Transaction rollback
- ASID uniqueness in batch

### Integration Tests
- Plugin using batch API via gRPC
- Large batch (1000+ attestations)
- Concurrent batch operations
- Error recovery

### Performance Tests
- Benchmark batch vs individual creation
- Measure transaction overhead
- Test SQLite transaction limits

## Open Questions

1. **Should batch API enforce a maximum batch size?**
   - Proposal: Soft limit of 1000, documented recommendation

2. **Should we expose transaction control to callers?**
   - Proposal: No, keep transactions internal for safety

3. **Should batch results include the original commands?**
   - Proposal: No, use indexed errors to map back to commands

4. **How should we handle duplicate ASIDs in a batch?**
   - Proposal: Treat as error, include in failure count

## Future Enhancements

### Streaming Batch API
For very large batches, consider streaming approach:

```go
type BatchBuilder interface {
    Add(cmd *types.AsCommand) error
    Flush() *BatchAttestationResult
    Close() error
}
```

### Batch Query API
Complement batch creation with batch querying:

```go
QueryBatch(filters []AttestationFilter) [][]*types.As
```

### Batch Update/Delete
If needed, extend batch pattern to updates and deletions:

```go
DeleteAttestations(asids []string) *BatchDeletionResult
```

## Conclusion

The batch attestation API provides:
- **Performance**: Faster bulk operations via transactions
- **Reliability**: Atomic operations with aggregate error handling
- **Simplicity**: Cleaner plugin code for bulk attestation creation

The implementation is straightforward, backward-compatible, and follows established QNTX patterns (similar to existing `BatchStore` interface).

**Recommendation**: Implement in phases, starting with core functionality and SQLStore implementation, then extend to plugin services.
