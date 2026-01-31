# ADR-003: Plugin Communication Patterns

**Status:** Accepted
**Date:** 2026-01-04
**Deciders:** QNTX Core Team

## Context

Domain plugins need to share data and coordinate work. How should plugins communicate?

**Requirements**:
1. Support both built-in and external (gRPC) plugins
2. Maintain plugin isolation (no tight coupling)
3. Enable async workflows (git ingestion triggers code analysis)
4. Provide consistency guarantees

## Decision

### Primary Communication: Database Attestations

**Plugins are isolated and communicate only via shared database:**

```
┌─────────────┐
│ Code Plugin │ ──┐
└─────────────┘   │
                  ├──> ┌──────────────┐ <──┐
┌─────────────┐   │    │  Attestation │    │
│Finance Plugin├───┘    │   Database   │    ├─── ┌──────────────┐
└─────────────┘        └──────────────┘    │    │ Biotech Plugin│
                                            └─── └──────────────┘
```

### Communication Patterns

#### 1. Event Sourcing via Attestations

Plugins write attestations to record events:

```go
// Code plugin creates attestation when git repo is ingested
attestation := &types.As{
    Actor:   "ixgest-git@user",
    Context: "repository_ingested",
    Entity:  "github.com/teranos/QNTX",
    Payload: json.RawMessage(`{"commit_count": 150, "language": "Go"}`),
}
store.Create(ctx, attestation)
```

Other plugins query attestations to discover events:

```go
// Finance plugin watches for new repositories
filter := &types.AxFilter{
    Context: ptr("repository_ingested"),
}
repos, err := store.Query(ctx, filter)
```

#### 2. State Sharing via Attestations

Plugins maintain state in attestations:

```go
// Code plugin updates file analysis state
attestation := &types.As{
    Actor:   "code-analyzer@system",
    Context: "file_analyzed",
    Entity:  "domains/code/plugin.go",
    Payload: json.RawMessage(`{
        "complexity": 12,
        "coverage": 85.3,
        "last_modified": "2026-01-04"
    }`),
}
```

#### 3. Async Workflows via Pulse Jobs

Long-running cross-plugin workflows use Pulse jobs:

```go
// Code plugin triggers dependency analysis job
job := &async.Job{
    HandlerName: "analyze_dependencies",
    Payload: json.RawMessage(`{"repo": "github.com/teranos/QNTX"}`),
    Source: "code-plugin",
}
queue.Enqueue(ctx, job)
```

### No Direct Plugin Communication

**Prohibited**:
```go
// ❌ WRONG: Direct plugin-to-plugin calls
financePlugin := registry.Get("finance")
financePlugin.(*finance.Plugin).AnalyzeRepository(repo)
```

**Rationale**:
- Tight coupling between plugins
- Breaks process isolation (gRPC plugins in different processes)
- Circular dependencies
- Difficult to version independently

**Correct approach**:
```go
// ✅ RIGHT: Communication via attestations
store.Create(ctx, &types.As{
    Actor:   "code@plugin",
    Context: "repository_ready",
    Entity:  repoURL,
    Payload: json.RawMessage(`{"trigger": "finance_analysis"}`),
})
```

### ServiceRegistry: Plugin ↔ QNTX Communication

Plugins interact with QNTX via ServiceRegistry:

```go
type ServiceRegistry interface {
    Database() *sql.DB              // Direct DB access
    Logger(domain string) *zap.SugaredLogger
    Config(domain string) Config
    ATSStore() *storage.SQLStore    // Attestation CRUD
}
```

This is the **only** interface plugins use to access QNTX.

## Consequences

### Positive

✅ **Decoupling**: Plugins can be added/removed without affecting others
✅ **Async by default**: Database acts as durable message queue
✅ **Consistency**: Database transactions provide ACID guarantees
✅ **Queryable**: Ax query language works across all plugin data
✅ **Audit trail**: All plugin interactions are attestations (queryable history)

### Negative

⚠️ **Latency**: Database round-trip slower than direct method calls
⚠️ **Complexity**: Developers must think in events/attestations, not procedure calls
⚠️ **Polling**: Plugins may need to poll for new attestations (mitigated by Pulse jobs)

### Neutral

- Attestation schema is flexible (JSON payload) but requires documentation
- Database is single point of contention (mitigated by SQLite write-ahead log)

## Examples

### Example 1: Git Ingestion → Code Analysis

```go
// 1. Code plugin ingests git repo
store.Create(ctx, &types.As{
    Actor:   "ixgest-git@user",
    Context: "repository_cloned",
    Entity:  "github.com/teranos/QNTX",
    Payload: json.RawMessage(`{"path": "/tmp/qntx", "branch": "main"}`),
})

// 2. Pulse job monitors for new repos and triggers analysis
// (This could be a scheduled job or a separate plugin watching attestations)

// 3. Analysis results also stored as attestations
store.Create(ctx, &types.As{
    Actor:   "code-analyzer@system",
    Context: "repository_analyzed",
    Entity:  "github.com/teranos/QNTX",
    Payload: json.RawMessage(`{"files": 250, "loc": 45000, "complexity": 3.2}`),
})
```

### Example 2: Cross-Domain Dependency

Finance plugin wants to analyze code complexity:

```go
// Finance plugin queries code plugin's attestations
filter := &types.AxFilter{
    Actor:   ptr("code-analyzer@system"),
    Context: ptr("repository_analyzed"),
    Entity:  ptr("github.com/teranos/QNTX"),
}

results, err := store.Query(ctx, filter)
// Parse results[0].Payload to get complexity metrics
```

No code→finance dependency, finance reads code's public data (attestations).

## Alternatives Considered

### Direct Service Registry Access
- **Rejected**: Tight coupling, breaks process isolation

### Event Bus (Redis Pub/Sub)
- **Rejected**: Adds external dependency, harder to query event history

### gRPC Direct Calls
- **Rejected**: Creates version coupling, harder to evolve plugins independently

### Shared In-Memory Cache
- **Rejected**: Doesn't work with process-isolated plugins

## Related

- [ADR-001: Domain Plugin Architecture](ADR-001-domain-plugin-architecture.md)
- [ADR-002: Plugin Configuration](ADR-002-plugin-configuration.md)
