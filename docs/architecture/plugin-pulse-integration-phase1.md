# Plugin-Pulse Integration: Phase 1 Complete

**Date:** 2026-01-31
**Branch:** `research/plugin-pulse-integration`
**Status:** ✅ Complete, builds successfully, backward compatible

## What Was Done

Phase 1 adds the protocol foundation for plugins to register async handlers with Pulse, without changing any runtime behavior. All changes are backward compatible.

### Protocol Changes

**File: `plugin/grpc/protocol/domain.proto`**

1. **Changed `Initialize` RPC return type:**
   ```protobuf
   - rpc Initialize(InitializeRequest) returns (Empty);
   + rpc Initialize(InitializeRequest) returns (InitializeResponse);
   ```

2. **Added `InitializeResponse` message:**
   ```protobuf
   message InitializeResponse {
     // Handler names this plugin can execute
     // Examples: ["python.script", "python.webhook", "ixgest.git"]
     // Empty list means plugin provides no async handlers (backward compatible)
     repeated string handler_names = 1;
   }
   ```

3. **Added `ExecuteJob` RPC:**
   ```protobuf
   rpc ExecuteJob(ExecuteJobRequest) returns (ExecuteJobResponse);
   ```

4. **Added job execution messages:**
   ```protobuf
   message ExecuteJobRequest {
     string job_id = 1;
     string handler_name = 2;
     bytes payload = 3;
     int64 timeout_secs = 4;
   }

   message ExecuteJobResponse {
     bool success = 1;
     string error = 2;
     bytes result = 3;
     int32 progress_current = 4;
     int32 progress_total = 5;
     double cost_actual = 6;
   }
   ```

### Go Implementation

**File: `plugin/grpc/client.go`**

1. Added `handlerNames []string` field to `ExternalDomainProxy`
2. Modified `Initialize()` to capture and store handler names from response
3. Added `GetHandlerNames()` method to expose handler names
4. Logs announced handlers at initialization

**File: `plugin/grpc/server.go`**

1. Updated `Initialize()` to return `InitializeResponse` instead of `Empty`
2. Returns empty handler list (Phase 1 stub)
3. Added `ExecuteJob()` stub that returns unimplemented error

### Rust Implementation

**File: `qntx-python/src/service.rs`**

1. Updated `initialize()` to return `InitializeResponse` instead of `Empty`
2. Returns empty handler list (Phase 1 stub) with comment for Phase 2
3. Added imports for new proto types: `InitializeResponse`, `ExecuteJobRequest`, `ExecuteJobResponse`
4. Added `execute_job()` stub that returns unimplemented error

### Code Generation

- Regenerated Go protobuf code via `make proto`
- Regenerated Rust protobuf code via Cargo build (automatic via `build.rs`)

## Backward Compatibility

✅ **No breaking changes:**
- All plugins return empty handler lists → no handlers registered
- `ExecuteJob` is never called (no handlers to route to)
- Existing functionality unchanged
- Both old and new protocol versions coexist

## Verification

✅ **All builds pass:**
- `go build ./cmd/qntx` - Success
- `make cli` - Success
- `cargo build --manifest-path=qntx-python/Cargo.toml` - Success (2 warnings, no errors)

## Next Steps (Phase 2)

See `docs/architecture/plugin-pulse-integration.md` for full roadmap.

**Phase 2 objectives:**
1. Python plugin implements `execute_job()` with actual script execution
2. Python plugin returns handler names: `["python.script", "python.webhook"]`
3. Create `pulse/async/plugin_proxy_handler.go` in Go
4. Wire proxy handlers during plugin initialization
5. Test end-to-end: job → Pulse → plugin → execution

## Related Discussion

During implementation, discussed architectural principle:

> **Protobuf as single source of truth for type definitions**

Currently QNTX has multiple sources of truth:
- Go structs with custom typegen for TypeScript
- Protobuf messages for plugin communication
- Manual TypeScript types

**Proposal:** Use protobuf as the single source, generate:
- Go types (via protoc-gen-go) ✅ already doing
- Rust types (via tonic_build) ✅ already doing
- TypeScript types (via protoc-gen-ts) ← not yet implemented
- Python types (via protoc-gen-python) ← if needed

This would eliminate typegen and ensure consistency across all languages.

Example: Attestation message could be defined once in protobuf, used everywhere.

**Benefits:**
- Single source of truth
- No drift between languages
- Standard tooling
- Cross-language compatibility guaranteed

**Consideration for future work.**

## Files Modified

```
modified:   plugin/grpc/protocol/domain.proto
modified:   plugin/grpc/protocol/domain.pb.go (generated)
modified:   plugin/grpc/protocol/domain_grpc.pb.go (generated)
modified:   plugin/grpc/client.go
modified:   plugin/grpc/server.go
modified:   qntx-python/src/service.rs
modified:   Cargo.lock
```

## Commits

1. `243ca16` - Research: Plugin-Pulse integration architecture
2. `5a11e6f` - Phase 1: Add plugin async handler protocol (backward compatible)
