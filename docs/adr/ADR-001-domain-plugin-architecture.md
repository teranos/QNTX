# ADR-001: Plugin Architecture

**Status:** Accepted (revised 2026-05-18)
**Date:** 2026-01-04
**Deciders:** QNTX Core Team

## Context

QNTX initially had software development functionality (git ingestion, GitHub integration, gopls language server, code editor) tightly coupled to the core. This created challenges:

1. **Scope Creep**: Core repository accumulated domain-specific code (code, biotech, finance, etc.)
2. **Coupling**: Domain logic mixed with core attestation/graph infrastructure
3. **Maintenance**: Changes to one domain risked breaking others
4. **Third-Party Extensions**: No mechanism for private/external domain implementations

## Decision

We adopt a **plugin architecture** where all non-core functionality is implemented as plugins. Every plugin implements the same interface and follows the same lifecycle. A plugin decides how much it implements — it can provide HTTP endpoints, WebSocket handlers, gRPC services, custom glyph types, or any combination. Improvements to QNTX core services benefit all plugins automatically.

### Minimal Core Philosophy

QNTX core is minimal and runs without any plugins:
- **Core components**: ATS (attestation system), Database (⊔), Pulse (꩜), Server
- **All domains are plugins**: Code, finance, legal, biotech, etc. are external plugins
- **Optional by default**: No plugins enabled in default configuration
- **Explicit opt-in**: Users configure which plugins to load via `am.toml`

This ensures QNTX core remains focused on infrastructure, not domain logic.

### Plugin Boundary

- Plugins are **isolated** — no shared memory, no direct method calls. Communication flows through core-mediated gRPC services and the shared attestation store.
- A plugin that provides a service (LLM, search, embedding, Python) registers it with core. Other plugins consume these services through `ServiceRegistry` without knowing which plugin provides them.
- No direct plugin-to-plugin calls — core mediates all inter-plugin communication.

### Plugin Model

All plugins are **external** — standalone binaries loaded at runtime via gRPC ([Plugin gRPC API](../api/grpc-plugin.md)):

- Plugins implement `DomainPlugin` interface inside their own binaries
- `ExternalDomainProxy` proxies gRPC calls to plugin processes
- `PluginServer` wrapper exposes the plugin's implementation via gRPC

From the Registry's perspective, all plugins are identical:
```go
registry.Register(externalProxy)
```

Plugin characteristics:
- Standalone binaries in `./qntx-plugins/` (first-party) or external repositories
- Communicate via gRPC only
- Configured via `am.toml` (whitelist model)
- Run in separate processes for isolation
- Discovered from configured search paths
- [Hot-swappable](../plugin-hot-swap.md) — enable/disable at runtime without server restart

### Interface Contract

The base interface every plugin implements:

```go
type DomainPlugin interface {
    Metadata() Metadata                                              // Plugin info, version
    Initialize(ctx context.Context, services ServiceRegistry) error  // Lifecycle
    Shutdown(ctx context.Context) error
    RegisterHTTP(mux *http.ServeMux) error                          // HTTP API
    RegisterWebSocket() (map[string]WebSocketHandler, error)        // Real-time features
    Health(ctx context.Context) HealthStatus                         // Monitoring
}
```

Optional interfaces extend the base — a plugin opts in by implementing them:

- `PausablePlugin` — pause/resume without full restart
- `ConfigurablePlugin` — exposes a config schema for UI-rendered settings
- `UIPlugin` — registers custom glyph types rendered on the canvas (see [`packages/glyphs`](https://github.com/teranos/QNTX/tree/main/packages/glyphs), [`hello-world plugin`](https://github.com/teranos/QNTX/tree/main/qntx-plugins/hello-world) for a minimal example)
- `LLMProvider` — LLM inference ([ADR-014](./ADR-014-llm-as-plugin-provided-service.md))
- `SearchProvider` — full-text search ([ADR-015](./ADR-015-search-as-plugin-provided-service.md))
- `EmbeddingProvider` — embedding and clustering ([ADR-017](./ADR-017-embedding-as-plugin-provided-service.md))
- `FetchService` — HTTP fetch with rate limiting and attestation creation
- `PythonService` — Python code execution ([ADR-022](./ADR-022-python-as-plugin-provided-service.md))

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

- **Namespace enforcement**: All plugin routes are mounted at `/api/<plugin-name>/*`
- **No conflicts by design**: Plugin namespaces prevent routing collisions
- **Roles**: Plugins declare roles on their routes (e.g., `llm-provider`). The UI discovers capabilities via `/api/plugins/routes` without hardcoding plugin names.

## Consequences

### Positive

- **Isolation**: Plugin failures don't cascade to core or other plugins
- **Scalability**: Each plugin can be developed/deployed independently
- **Third-party**: External developers can build private plugins
- **Process isolation**: Plugin crashes don't crash QNTX server
- **Language agnostic**: gRPC enables plugins in any language (Go, Python, TypeScript, Rust)
- **Core improvements propagate**: Service-level improvements (queuing, rate limiting, observability) benefit all plugins automatically

### Negative

- **gRPC overhead**: Extra hop for plugin calls vs in-process
- **Complexity**: More moving parts (multiple processes, config files, gRPC)
- **No shared state**: Plugins can't share in-memory caches (intentional isolation)

### Neutral

- First-party plugins in `qntx-plugins/` follow the same patterns as third-party plugins
- `hello-world` plugin serves as the minimal reference implementation

## Implementation History

- **PR #130**: `DomainPlugin` interface, Registry, ServiceRegistry, HTTP/WebSocket registration
- **PR #134**: Server decoupled from plugin internals, dynamic handler registration
- **PR #136**: Plugin discovery from search paths, `am.toml` whitelist, gRPC-only communication, minimal core mode
- **Plugin-provided services**: LLM (ADR-014), Search (ADR-015), Vector Search (ADR-016), Embedding (ADR-017), Graph (ADR-021), Python (ADR-022)
- **Hot-swap**: Plugins can be enabled/disabled at runtime via `am.toml` changes or API

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
- [Plugin Hot-Swap](../plugin-hot-swap.md) — runtime enable/disable via am.toml or API
