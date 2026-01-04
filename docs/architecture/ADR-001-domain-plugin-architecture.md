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

### Plugin Boundary

- **One plugin per domain** (code, biotech, finance, legal, etc.)
- Each plugin encapsulates all domain functionality (ingestion, API, UI, language servers)
- Plugins are **isolated** - they communicate only via shared database (attestations)
- No direct plugin-to-plugin method calls or dependencies

### Unified Plugin Model

There is **one interface**: `DomainPlugin`. Two implementations:

- **Built-in plugins**: Implement `DomainPlugin` directly (compiled into QNTX)
- **External plugins**: `ExternalDomainProxy` adapter implements `DomainPlugin` by proxying gRPC calls to sidecar processes

From the Registry's perspective, all plugins are identical:
```go
// Both work the same way
registry.Register(code.NewPlugin())           // Built-in
registry.Register(externalProxy)              // External (via gRPC)
```

External plugins:
- Are standalone binaries with their own repos
- Communicate via gRPC (using `PluginServer` wrapper)
- Have separate config files (`am.<domain>.toml`)
- Run in separate processes for isolation

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

- **Fail-fast initialization**: Server refuses to start if any plugin fails to initialize
- **Runtime error notification**: Plugin errors broadcast to UI for user awareness
- Rationale: Clear failure signals prevent silent degradation; plugins are essential features

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

- Built-in plugins (code domain) use same DomainPlugin interface for consistency
- Code domain serves as reference implementation and architecture validation

## Implementation Plan

### Phase 1: Internal Plugin API (Completed)
- ✅ DomainPlugin interface and Registry
- ✅ ServiceRegistry for dependency injection
- ✅ Code domain as built-in plugin
- ✅ CLI, HTTP, and config integration

### Phase 2: gRPC Transport (Completed)
- ✅ Generate Go code from `domains/grpc/protocol/domain.proto`
- ✅ `ExternalDomainProxy` adapter (implements DomainPlugin via gRPC)
- ✅ `PluginServer` wrapper (exposes DomainPlugin via gRPC)
- ✅ `PluginManager` for plugin discovery and process management
- ✅ Code domain as standalone binary (`cmd/plugins/code`)
- ✅ Plugin development documentation

### Phase 3: Third-Party Plugins (Future)
- Publish plugin development guide
- Release QNTX plugin SDK
- Support community plugins (biotech, finance, legal)

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
