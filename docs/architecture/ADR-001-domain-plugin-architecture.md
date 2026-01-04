# ADR-001: Domain Plugin Architecture

**Status:** Accepted
**Date:** 2026-01-04
**Deciders:** QNTX Core Team

## Context

QNTX initially had software development functionality (git ingestion, GitHub integration, gopls language server, code editor) tightly coupled to the core. This created challenges:

1. **Scope Creep**: Core repository accumulated domain-specific code (code, biotech, finance, etc.)
2. **Coupling**: Domain logic mixed with core attestation/graph infrastructure
3. **Maintenance**: Changes to one domain risked breaking others
4. **Third-Party Extensions**: No mechanism for private/external domain implementations

## Decision

We adopt a **domain plugin architecture** where functional domains are implemented as plugins with:

### Minimal Core Philosophy

QNTX core is minimal and runs without any plugins:
- **Core components**: ATS (attestation system), Database (⊔), Pulse (꩜), Server
- **All domains are plugins**: Code, finance, legal, biotech, etc. are external plugins
- **Optional by default**: No plugins enabled in default configuration
- **Explicit opt-in**: Users configure which plugins to load via `am.toml`

This ensures QNTX core remains focused on infrastructure, not domain logic.

### Plugin Boundary

- **One plugin per domain** (code, biotech, finance, legal, etc.)
- Each plugin encapsulates all domain functionality (ingestion, API, UI, language servers)
- Plugins are **isolated** - they communicate only via shared database (attestations)
- No direct plugin-to-plugin method calls or dependencies

### Unified Plugin Model

All plugins are **external** - standalone binaries loaded at runtime via gRPC:

- Plugins implement `DomainPlugin` interface inside their own binaries
- `ExternalDomainProxy` adapter implements `DomainPlugin` by proxying gRPC calls to plugin processes
- `PluginServer` wrapper exposes plugin's `DomainPlugin` implementation via gRPC

From the Registry's perspective, all plugins are identical:
```go
// All plugins registered the same way
registry.Register(externalProxy)  // Loaded via gRPC from ~/.qntx/plugins/
```

Plugin characteristics:
- Standalone binaries with their own repositories
- Communicate via gRPC only (using `PluginServer` wrapper)
- Configured via main `am.toml` file (whitelist model)
- Run in separate processes for isolation
- Discovered from configured search paths

### Interface Contract

```go
type DomainPlugin interface {
    Metadata() Metadata                                              // Plugin info, version
    Initialize(ctx context.Context, services ServiceRegistry) error  // Lifecycle
    Shutdown(ctx context.Context) error
    Commands() []*cobra.Command                                      // CLI integration
    RegisterHTTP(mux *http.ServeMux) error                          // HTTP API
    RegisterWebSocket() (map[string]WebSocketHandler, error)        // Real-time features
    Health(ctx context.Context) HealthStatus                         // Monitoring
}
```

### Security Model

- **gRPC sandbox**: Plugins run in separate processes with limited access
- **Service Registry**: Plugins access QNTX via controlled ServiceRegistry interface
- **No direct access**: Plugins cannot access QNTX internals, only exposed services

### Failure Handling

- **Optional by default**: QNTX runs in minimal core mode without any plugins
- **Warning on failure**: If enabled plugin fails to load, log warning and continue without it
- **Runtime error notification**: Plugin errors broadcast to UI for user awareness
- Rationale: Minimal core philosophy - plugins are optional enhancements, not essential features

### Versioning

- **Strict semver**: Plugins declare required QNTX version using semver constraints
- **Validation at registration**: Registry checks compatibility before loading plugin
- Breaking changes in QNTX API trigger major version bump

### HTTP Routing

- **Namespace enforcement**: All plugin routes must be `/api/<domain>/*`
- **No conflicts by design**: Domain namespaces prevent routing collisions
- Example: code plugin owns all `/api/code/*` routes

## Consequences

### Positive

✅ **Isolation**: Domain failures don't cascade to core or other domains
✅ **Scalability**: Each plugin can be developed/deployed independently
✅ **Third-party**: External developers can build private plugins (finance, legal, etc.)
✅ **Process isolation**: Plugin crashes don't crash QNTX server
✅ **Language agnostic**: gRPC enables plugins in any language (Go, Python, Rust)

### Negative

⚠️ **gRPC overhead**: Extra hop for plugin calls vs in-process
⚠️ **Complexity**: More moving parts (multiple processes, config files, gRPC)
⚠️ **No shared state**: Plugins can't share in-memory caches (intentional isolation)

### Neutral

- Code domain (qntx-code-plugin) serves as reference implementation
- First-party plugins maintained by QNTX team follow same patterns as third-party plugins

## Implementation Plan

### Phase 1: Restructure to plugin/ (Completed - PR #130)
- ✅ DomainPlugin interface and Registry
- ✅ ServiceRegistry for dependency injection
- ✅ Moved domains → plugin, code → qntx-code
- ✅ HTTP and WebSocket handler registration

### Phase 2: Decouple Server Handlers (Completed - PR #134)
- ✅ Migrated gopls lifecycle to qntx-code plugin
- ✅ Server dynamically registers plugin WebSocket handlers
- ✅ Removed server code dependencies (350+ lines)
- ✅ Server now domain-agnostic

### Phase 3: Plugin Discovery and Optional Loading (Completed - PR #136)
- ✅ Plugin configuration via `am.toml` (whitelist model)
- ✅ Plugin discovery from configured search paths
- ✅ Removed all built-in plugin fallbacks
- ✅ QNTX runs in minimal core mode without plugins
- ✅ gRPC-only communication (no Go plugin .so files)

### Phase 4: Extract to Separate Repository (Planned - Issue #135)
- Create `teranos/qntx-code-plugin` repository
- Extract `qntx-code/` from QNTX monorepo
- Distribute plugin binaries via GitHub releases
- Plugin development guide and documentation

## Alternatives Considered

### Monolithic with Feature Flags
- **Rejected**: Doesn't solve coupling, still requires rebuilding for extensions

### Microservices per Domain
- **Rejected**: Too heavyweight, networking complexity, no shared DB access

### Lua/WebAssembly Scripting
- **Rejected**: Limited to small extensions, not suitable for full domains (gopls, git)

## Related

- [ADR-002: Plugin Configuration Management](./ADR-002-plugin-configuration.md)
- [ADR-003: Plugin Communication Patterns](./ADR-003-plugin-communication.md)
- PR #130: Domain Plugin Architecture
- Issue #128: Implement gRPC Protocol and Externalize Code Domain
