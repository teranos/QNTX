# Plugin-Pulse Integration: Enabling Plugins to Register Async Handlers

**Status:** Phase 1 & 2 Complete ✓
**Date:** 2026-01-31
**Branch:** `research/plugin-pulse-integration`

## Implementation Status

### ✅ Phase 1: Protocol Foundation (COMPLETE)
**Goal:** Enable plugins to announce async handlers and receive execution requests

**Implemented:**
- Updated `domain.proto`: `Initialize` returns `InitializeResponse` with `handler_names[]`
- Added `ExecuteJob` RPC to protocol
- Regenerated Go/Rust protobuf code
- Backward compatible: Empty handler lists work fine

### ✅ Phase 2: Plugin Execution Infrastructure (COMPLETE)
**Goal:** Python plugin can execute jobs forwarded by Pulse

**Implemented:**
- Python plugin announces `["python.script"]` in `initialize()`
- Python plugin implements `execute_job()` RPC
- `PluginProxyHandler` in Go forwards jobs to plugins via gRPC
- `server/init.go` auto-registers plugin handlers with Pulse
- Removed hardcoded `qntx-code` import from `server/ats_parser.go`

**Architecture now working:**
```
Job with handler_name="python.script"
  → Pulse Worker picks up job
  → PluginProxyHandler.Execute()
  → gRPC call to Python plugin
  → Plugin executes code
  → Returns result/progress/cost to Pulse
```

### 🚧 Phase 3: Dynamic Handler Discovery (NEXT)
**Goal:** Python plugin reads saved scripts from ATS store and announces them as handlers

**Plan (needs validation):**
1. Define attestation schema for saved handler scripts
2. Python plugin scans ATS store during initialization
3. For each saved handler script, announce `"python.<handler_name>"`
4. Example: Script saved as "vacancies" → announces `"python.vacancies"`

### 🚧 Phase 4: Handler Glyph
**Goal:** User can create/edit handler scripts via UI

**Plan (needs validation):**
1. New Handler Glyph (shares infrastructure with Python Glyph)
2. Template generation for new handlers
3. Save handler script to ATS store as attestation
4. Trigger plugin re-initialization to announce new handler

### 🚧 Phase 5: ATS Parser Integration
**Goal:** `ix <command>` syntax discovers and routes to handlers

**Plan (needs validation):**
1. ATS parser detects unknown `ix <command>`
2. System prompts: "Handler not found. Create handler?"
3. Opens Handler Glyph on user confirmation
4. After saving, route `ix <command>` to `python.<command>`

---

**Original Context:** Feature branch `feature/dynamic-ix-routing` attempts to add Python script execution via Pulse, but does so through an ad-hoc Go handler that makes gRPC calls to the plugin. This document proposes a general architecture for plugins to register async handlers directly with Pulse.

## Problem Statement

### Current Architecture

Pulse (async job system) and the Plugin system exist as parallel, largely disconnected systems:

```
┌─────────────────────────────────────────────┐
│ QNTX Core                                   │
│                                             │
│  ┌──────────────┐      ┌─────────────────┐ │
│  │   Pulse      │      │  Plugin System  │ │
│  │              │      │                 │ │
│  │ - Registry   │      │ - Python plugin │ │
│  │ - Workers    │      │ - Code plugin   │ │
│  │ - Queue      │◄─────┤   (can enqueue) │ │
│  └──────────────┘      └─────────────────┘ │
│         │                                   │
│         │ Handlers registered in Go:        │
│         ├─ ixgest.git (qntx-code package)   │
│         └─ python.script (NEW, ad-hoc) ❌   │
│                  (Go code calls plugin)     │
└─────────────────────────────────────────────┘
```

**Current state:**
- Plugins can **enqueue** jobs (via `queue_endpoint` passed during `Initialize`)
- Pulse cannot route jobs **to** plugins for execution
- Adding plugin-based async handlers requires:
  1. Writing a Go handler in `pulse/async/`
  2. That handler manually calls the plugin via gRPC
  3. Registering the Go handler in `cmd/qntx/commands/pulse.go`

**Example from `feature/dynamic-ix-routing`:**
```go
// cmd/qntx/commands/pulse.go:94
registry.Register(async.NewPythonScriptHandler(pythonURL, logger.Logger))
```

This creates a Go shim (`PythonScriptHandler`) that forwards execution to the Python plugin.

### Why This Is Problematic

1. **Not scalable**: Each plugin capability needs a custom Go handler
2. **Violates plugin architecture**: Plugins should define their own capabilities
3. **Tight coupling**: Go code needs to know about plugin endpoints
4. **Duplication**: Handler logic lives partly in Go, partly in plugin
5. **Against "minimal core" philosophy**: Domain logic (Python execution) leaks into core

### What Plugins Already Have

Plugins receive service endpoints during initialization:

```protobuf
message InitializeRequest {
  string ats_store_endpoint = 1;  // For creating attestations
  string queue_endpoint = 2;       // For enqueuing jobs ✓
  string auth_token = 3;
  map<string, string> config = 4;
}
```

**Plugins can already:**
- Enqueue jobs via `QueueService` gRPC
- Query/update job status
- Access attestation store

**Plugins cannot:**
- Register themselves as job handlers
- Receive jobs from Pulse for execution
- Advertise their async capabilities

## Proposed Architecture

### High-Level Design

Enable bidirectional integration between Pulse and plugins:

```
┌─────────────────────────────────────────────┐
│ QNTX Core                                   │
│                                             │
│  ┌──────────────┐      ┌─────────────────┐ │
│  │   Pulse      │◄────►│  Plugin System  │ │
│  │              │      │                 │ │
│  │ - Registry   │      │ - Python plugin │ │
│  │ - Workers    │      │   announces:    │ │
│  │ - Queue      │      │   "python.*"    │ │
│  │              │      │                 │ │
│  │ Handlers:    │      │ - Code plugin   │ │
│  │ ├─ ixgest.git│      │   announces:    │ │
│  │ │  (Go)      │      │   "ixgest.git"  │ │
│  │ └─ python.*  │      │                 │ │
│  │    (proxied  │──────►                 │ │
│  │     to plugin)       └─────────────────┘ │
└─────────────────────────────────────────────┘
```

**Key concepts:**

1. **Plugins announce handler capabilities** during initialization
2. **Pulse creates proxy handlers** that forward execution to plugins
3. **Job routing is automatic** based on `job.HandlerName`
4. **No Go code needed** for new plugin-provided handlers

### Protocol Changes

#### 1. Add Handler Registration to Plugin Protocol

Modify `plugin/grpc/protocol/domain.proto`:

```protobuf
// Add to InitializeRequest response
message InitializeResponse {
  // Handler names this plugin can execute
  // Examples: ["python.script", "python.webhook", "python.analysis"]
  repeated string handler_names = 1;
}

// NEW: RPC for executing jobs
service DomainPluginService {
  // ... existing RPCs ...

  // ExecuteJob executes an async job
  rpc ExecuteJob(ExecuteJobRequest) returns (ExecuteJobResponse);
}

message ExecuteJobRequest {
  string job_id = 1;           // For logging/tracking
  string handler_name = 2;     // Which handler to invoke
  bytes payload = 3;           // Job-specific data (JSON)
  int64 timeout_secs = 4;      // Execution timeout
}

message ExecuteJobResponse {
  bool success = 1;
  string error = 2;            // Error message if failed
  bytes result = 3;            // Optional result data

  // Progress tracking (optional)
  int32 progress_current = 4;
  int32 progress_total = 5;

  // Cost tracking (optional)
  double cost_actual = 6;
}
```

**Design notes:**
- `Initialize` now returns handler names instead of `Empty`
- New `ExecuteJob` RPC allows Pulse to invoke plugin handlers
- Response includes progress/cost for Pulse to update job state

#### 2. Plugin Implementation Changes

Plugins implement handler registration:

**Example: Python plugin** (`qntx-python/src/service.rs`)

```rust
#[tonic::async_trait]
impl DomainPluginService for PythonPluginService {
    async fn initialize(&self, request: Request<InitializeRequest>)
        -> Result<Response<InitializeResponse>, Status> {

        // ... existing initialization logic ...

        // Announce async handler capabilities
        Ok(Response::new(InitializeResponse {
            handler_names: vec![
                "python.script".to_string(),
                "python.webhook".to_string(),
                "python.csv".to_string(),
            ],
        }))
    }

    async fn execute_job(&self, request: Request<ExecuteJobRequest>)
        -> Result<Response<ExecuteJobResponse>, Status> {

        let req = request.into_inner();

        // Route to internal handler based on handler_name
        match req.handler_name.as_str() {
            "python.script" => self.execute_python_script(req).await,
            "python.webhook" => self.execute_webhook_handler(req).await,
            "python.csv" => self.execute_csv_handler(req).await,
            _ => Err(Status::not_found(format!(
                "Unknown handler: {}", req.handler_name
            ))),
        }
    }
}
```

#### 3. Pulse Changes: Plugin Proxy Handler

Create a generic proxy handler in `pulse/async/`:

**New file: `pulse/async/plugin_proxy_handler.go`**

```go
package async

import (
    "context"
    "github.com/teranos/QNTX/plugin/grpc"
    "github.com/teranos/QNTX/plugin/grpc/protocol"
)

// PluginProxyHandler forwards job execution to a plugin via gRPC
type PluginProxyHandler struct {
    handlerName string
    plugin      *grpc.ExternalDomainProxy
}

func NewPluginProxyHandler(handlerName string, plugin *grpc.ExternalDomainProxy) *PluginProxyHandler {
    return &PluginProxyHandler{
        handlerName: handlerName,
        plugin:      plugin,
    }
}

func (h *PluginProxyHandler) Name() string {
    return h.handlerName
}

func (h *PluginProxyHandler) Execute(ctx context.Context, job *Job) error {
    // Forward job to plugin via gRPC
    req := &protocol.ExecuteJobRequest{
        JobId:       job.ID,
        HandlerName: h.handlerName,
        Payload:     job.Payload,
        TimeoutSecs: 300, // TODO: configurable
    }

    resp, err := h.plugin.Client().ExecuteJob(ctx, req)
    if err != nil {
        return errors.Wrap(err, "plugin execution failed")
    }

    if !resp.Success {
        return errors.New(resp.Error)
    }

    // Update job progress/cost from plugin response
    if resp.ProgressTotal > 0 {
        job.Progress = Progress{
            Current: int(resp.ProgressCurrent),
            Total:   int(resp.ProgressTotal),
        }
    }

    if resp.CostActual > 0 {
        job.CostActual = resp.CostActual
    }

    return nil
}
```

#### 4. Plugin Manager Integration

Modify plugin initialization to register handlers with Pulse:

**Updated: `server/init.go` or plugin initialization code**

```go
func initializePlugins(ctx context.Context, db *sql.DB, logger *zap.SugaredLogger) error {
    pluginManager := grpcplugin.NewPluginManager(logger)

    // Load plugins from config
    if err := pluginManager.LoadPlugins(ctx, pluginConfigs); err != nil {
        return err
    }

    // Create Pulse handler registry
    pulseRegistry := async.NewHandlerRegistry()

    // Register handlers from each plugin
    for _, plugin := range pluginManager.GetAllPlugins() {
        metadata := plugin.Metadata()

        // Get handler names from plugin (via Initialize response)
        handlerNames := plugin.GetHandlerNames() // NEW method

        for _, handlerName := range handlerNames {
            logger.Infof("Registering plugin handler: %s (from %s plugin)",
                handlerName, metadata.Name)

            proxyHandler := async.NewPluginProxyHandler(handlerName, plugin)
            pulseRegistry.Register(proxyHandler)
        }
    }

    return nil
}
```

### Migration Path

**Phase 1: Add protocol support (no behavior change)**
1. Add `InitializeResponse` with `handler_names` to protobuf
2. Add `ExecuteJob` RPC to protobuf
3. Regenerate Go/Rust protobuf code
4. Update plugins to return `InitializeResponse` (empty list for now)

**Phase 2: Implement plugin-side handlers**
1. Python plugin implements `execute_job` method
2. Returns handler names in `initialize` response
3. Internal routing to script execution logic

**Phase 3: Implement Pulse proxy handlers**
1. Create `PluginProxyHandler` in Go
2. Register proxies during plugin initialization
3. Test with Python plugin handlers

**Phase 4: Remove ad-hoc handlers**
1. Remove `pulse/async/python_handler.go` (from branch #2)
2. Remove manual registration in `cmd/qntx/commands/pulse.go`
3. All plugin handlers now automatic

**Phase 5: Migrate existing handlers**
1. Move `ixgest.git` handler into `qntx-code` plugin
2. Remove from Go codebase
3. Pure plugin-based async execution

## Benefits

### 1. Extensibility
- New plugin = new async capabilities automatically
- No Go code changes needed for plugin features
- Third-party plugins can provide handlers

### 2. Clean Architecture
- Pulse = generic job queue/router
- Plugins = domain-specific execution logic
- Clear separation of concerns

### 3. Consistency with Plugin Philosophy
- Plugins already handle HTTP/WebSocket endpoints
- Async execution is natural extension
- Follows existing `ServiceRegistry` pattern

### 4. Enables Dynamic IX Routing
- Plugins register IX handlers via attestations
- `ats_parser.go` queries attestations for handlers
- Plugin executes script when job runs
- Full stack: attestation → job → plugin → execution

### 5. Reduced Core Complexity
- No domain-specific code in `pulse/async/`
- Handlers live where they belong (in plugins)
- Easier to maintain and test

## Risks and Mitigation

### Risk 1: Plugin Crashes During Execution

**Scenario:** Plugin process crashes while executing a job

**Mitigation:**
- Pulse timeout mechanism already exists
- Job marked as failed after timeout
- Plugin restart logic (already exists in plugin manager)
- Worker pool continues with other jobs

### Risk 2: Handler Name Conflicts

**Scenario:** Two plugins claim the same handler name

**Mitigation:**
- Handler registry panics on duplicate registration (existing behavior)
- Plugin loading fails early (during startup)
- Clear error message identifies conflicting plugins
- **Alternative:** Namespace handlers by plugin name (`python.python.script` vs `rust.python.script`)

### Risk 3: Performance Overhead

**Scenario:** gRPC call overhead for every job execution

**Analysis:**
- gRPC is already used for HTTP requests to plugins
- Overhead is minimal for typical async jobs (seconds/minutes duration)
- No worse than current ad-hoc approach (still gRPC)

**Mitigation:**
- Benchmark plugin execution vs native Go handlers
- Consider connection pooling if needed
- Handlers are long-lived, connection reused

### Risk 4: Backward Compatibility

**Scenario:** Existing deployments break

**Mitigation:**
- Phase 1 adds protocol, doesn't change behavior
- Old plugins return empty handler list (no-op)
- Gradual migration of handlers
- Both approaches can coexist during transition

## Open Questions

1. **Handler discovery**: Should plugins announce handlers dynamically (via attestations) or statically (via Initialize)?
   - **Static** (proposed): Simpler, less overhead, handlers known at startup
   - **Dynamic**: More flexible, handlers can be added/removed at runtime

2. **Handler namespacing**: Should handler names include plugin name prefix?
   - **No prefix** (proposed): Cleaner names, plugins own their namespace
   - **With prefix**: Prevents conflicts, clearer ownership (`python:script`)

3. **Job lifecycle**: Should plugins update job status themselves?
   - **No** (proposed): Pulse manages job state, plugin just executes
   - **Yes**: Plugin has full control, more flexible but complex

4. **Timeouts**: Who enforces timeout - Pulse or plugin?
   - **Pulse** (proposed): Context cancellation, consistent across handlers
   - **Plugin**: More control but inconsistent behavior

5. **Existing Go handlers**: Migrate all to plugins, or keep some in core?
   - **Migrate** (proposed): Clean architecture, everything in plugins
   - **Hybrid**: Keep critical handlers in Go for reliability

## Implementation Checklist

### Protobuf Changes
- [ ] Add `InitializeResponse` message with `handler_names` field
- [ ] Add `ExecuteJob` RPC to `DomainPluginService`
- [ ] Add `ExecuteJobRequest` and `ExecuteJobResponse` messages
- [ ] Regenerate Go code: `make proto` or similar
- [ ] Regenerate Rust code in `qntx-python/`

### Plugin Changes (Python)
- [ ] Update `initialize()` to return `InitializeResponse`
- [ ] Implement `execute_job()` RPC handler
- [ ] Route to internal handlers based on `handler_name`
- [ ] Return success/error/progress/cost in response
- [ ] Update tests

### Pulse Changes
- [ ] Create `pulse/async/plugin_proxy_handler.go`
- [ ] Add `GetHandlerNames()` method to `ExternalDomainProxy`
- [ ] Update plugin initialization to register proxy handlers
- [ ] Add tests for proxy handler execution

### Integration
- [ ] Update `server/init.go` to wire plugin handlers to Pulse
- [ ] Test with Python plugin executing `python.script` jobs
- [ ] Verify job status updates correctly
- [ ] Verify timeout handling
- [ ] Verify error propagation

### Documentation
- [ ] Update `docs/api/grpc-plugin.md` with new RPC
- [ ] Update `docs/development/external-plugin-guide.md`
- [ ] Add examples of implementing async handlers in plugins
- [ ] Update architecture diagrams

### Cleanup (Optional Phase 4)
- [ ] Remove `pulse/async/python_handler.go` (from feature branch)
- [ ] Remove manual handler registration in `cmd/qntx/commands/pulse.go`
- [ ] Consider migrating `ixgest.git` to `qntx-code` plugin

## Comparison with Branch #2 (Python Script Async Handler)

**Branch #2 approach:**
```
Job "python.script"
  ↓
Pulse worker
  ↓
PythonScriptHandler (Go)
  ↓
HTTP POST to /api/python/execute (actually gRPC)
  ↓
Python plugin
```

**Proposed approach:**
```
Job "python.script"
  ↓
Pulse worker
  ↓
PluginProxyHandler (Go)
  ↓
ExecuteJob RPC
  ↓
Python plugin
```

**Differences:**
- **Branch #2**: Ad-hoc handler, custom gRPC call, one-off solution
- **Proposed**: Generic handler, standard protocol, reusable pattern

**Effort comparison:**
- **Branch #2**: 2 files, works for one case
- **Proposed**: 4-5 files, works for ALL plugin handlers forever

## Related Work

- **ADR-001**: Domain Plugin Architecture - establishes plugin system
- **ADR-002**: Plugin Configuration - how plugins are discovered/loaded
- **Feature branch #1**: Structured Error Details - enables rich error info from plugins
- **Feature branch #2**: Python Script Async Handler - ad-hoc solution this proposal generalizes
- **Feature branch #4**: Dynamic IX Routing - primary use case for plugin handlers

## Next Steps

1. **Validate proposal** with team/maintainer
2. **Prototype** Phase 1 (protobuf changes only)
3. **Implement** Python plugin handler support (Phase 2)
4. **Test** with dynamic IX routing use case
5. **Document** pattern for other plugins
6. **Consider** migrating existing Go handlers to plugins

## Conclusion

Plugin-Pulse integration is a natural evolution of QNTX's plugin architecture. By enabling plugins to register async handlers, we:
- Extend the plugin system consistently
- Eliminate ad-hoc integration code
- Enable powerful features like dynamic IX routing
- Maintain clean separation between core and domain logic

The implementation is straightforward, backward compatible, and provides immediate value for the Python plugin while establishing a pattern for all future plugins.
