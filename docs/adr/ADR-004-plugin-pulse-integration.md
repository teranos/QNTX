# ADR-004: Plugin-Pulse Integration for Dynamic Async Handlers

**Status:** Accepted
**Date:** 2026-01-31
**Deciders:** QNTX Core Team
**Related:** ADR-001 (Domain Plugin Architecture)

## Context

Pulse (async job system) and the Plugin system existed as parallel systems. Plugins could enqueue jobs but not execute them. Adding plugin-based async capabilities required writing Go handler shims that manually called plugins via gRPC, violating the "minimal core" philosophy and plugin architecture.

### Problem

```
Current: Plugin wants async capability
  → Write Go handler in pulse/async/
  → Go handler calls plugin via gRPC
  → Register Go handler manually
  → Domain logic leaks into core
```

This pattern:
- Doesn't scale (one Go shim per plugin capability)
- Violates plugin architecture (plugins should define capabilities)
- Creates tight coupling (Go code knows plugin endpoints)
- Duplicates logic (handler behavior split across Go and plugin)

## Decision

Enable bidirectional integration: plugins register async handlers directly with Pulse, eliminating Go shims.

### Architecture

```
New: Plugin announces async handlers
  → Server registers handlers with Pulse
  → Pulse routes jobs to plugins via gRPC
  → Plugin executes and returns results
  → No Go domain logic required
```

### Protocol Changes

Extended `domain.proto`:
- `Initialize` returns `InitializeResponse` with `handler_names[]`
- New `ExecuteJob` RPC for plugin execution
- Backward compatible (empty handler lists work)

### Handler Registration Flow

1. **Plugin startup:** Plugin loads, announces capabilities
2. **After initialization:** Server registers announced handlers with Pulse
3. **Job execution:** Pulse routes to plugin via `PluginProxyHandler`
4. **Results:** Plugin returns progress/cost/result to Pulse

## Implementation Phases

### Phase 1: Protocol Foundation ✅
- Protocol changes for handler announcement
- `ExecuteJob` RPC definition
- Backward compatible with existing plugins

### Phase 2: Plugin Execution ✅
- Python plugin (qntx-python v0.4.0) implements `execute_job()`
- `PluginProxyHandler` forwards jobs to plugins
- Removed hardcoded domain imports from core

### Phase 3: Dynamic Handler Discovery ✅
- CLI: `qntx handler create` stores handlers as attestations
- Python plugin (qntx-python v0.4.0) queries ATS store during initialization
- Handler code extracted from attestation `attributes.code`
- Discovered handlers stored in plugin state and announced automatically
- Self-certifying pattern (handler is own actor)
- Dynamic routing in `execute_job()` handles discovered handlers

### Phase 4: Registration Timing ✅
- Fixed async race (Pulse starts before plugins load)
- Moved registration to after plugin initialization
- Added `GetDaemon()` for registry access
- Handlers properly registered with Pulse

## Consequences

### Positive

**Extensibility:** New plugin = new async capabilities automatically
- No Go code changes needed
- Third-party plugins can provide handlers
- Consistent pattern across all plugins

**Clean Architecture:**
- Pulse remains generic job router
- Domain logic stays in plugins
- Clear separation of concerns

**Dynamic Capabilities:**
- Users create handlers via CLI
- Stored as attestations
- Discovered and registered automatically
- No code deployment required

**Reduced Core Complexity:**
- No domain-specific code in `pulse/async/`
- Handlers live where they belong
- Easier to maintain and test

### Negative

**gRPC Overhead:**
- Every job execution requires gRPC call
- Minimal for typical async jobs (seconds/minutes duration)
- Acceptable tradeoff for architectural benefits

**Plugin Crashes:**
- Plugin crash affects job execution
- Mitigated by: existing timeout mechanism, plugin restart logic, worker pool isolation

**Version Compatibility:**
- Protocol changes require coordination
- Mitigated by: backward compatibility, gradual rollout

## Alternatives Considered

### 1. Keep Go Handler Shims
**Rejected:** Violates plugin architecture, doesn't scale, duplicates logic

### 2. Plugin SDK for Direct Queue Access
**Rejected:** Bypasses Pulse routing, loses centralized job management, no progress tracking

### 3. Event-Based Integration
**Rejected:** More complex, harder to debug, less direct control flow

## Related Decisions

- **ADR-001:** Established plugin architecture foundation
- **Phase 5 (Future):** Migrate `ixgest.git` to code plugin for pure plugin-based system

## Notes

This decision removes the last barrier to a fully plugin-based async execution system. Domain logic (Python execution, git ingestion, etc.) now lives entirely in plugins, with core QNTX providing only generic infrastructure (routing, queuing, progress tracking, budget management).

The self-certifying attestation pattern for handlers enables unlimited user-created handlers without hitting bounded storage limits, as each handler acts as its own actor.
