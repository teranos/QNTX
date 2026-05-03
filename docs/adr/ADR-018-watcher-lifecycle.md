# ADR-018: Plugin Lifecycle, Watchers, and Developer Experience

**Status:** Accepted
**Date:** 2026-05-03
**Deciders:** QNTX Core Team
**Related:** ADR-003 (Plugin Communication), ADR-004 (Plugin-Pulse Integration)

## Context

Plugins are separate processes that communicate with QNTX over gRPC. The lifecycle — boot, initialization, health monitoring, restart, shutdown — has implicit contracts that caused real bugs: double initialization, stale connections, silent watcher failures. This ADR documents the full plugin lifecycle and the watcher subsystem as canonical references.

### Problems observed

1. **Double initialization.** `ForceInitialize` bypassed `initOnce` without consuming it. The HTTP routing lazy-init then called `Initialize` again, causing plugins to start their work twice and tear down what they just built.

2. **WebSocket ping-pong is undocumented.** Plugins implementing `HandleWebSocket` must read the incoming gRPC stream and respond to PING messages with PONG. If they ignore the stream (natural first instinct), QNTX logs "WebSocket pong timeout" and the connection dies.

3. **Watcher lifecycle is implicit.** The full path — `InitializeResponse.watchers` → DB → engine → `ExecuteJob` — is spread across four files. A plugin developer sees only the proto field and `ExecuteJob`.

4. **Predicate matching rules are undocumented.** Matching semantics (exact, OR, rate limiting) are only discoverable by reading `engine.go`.

## Decision

Document the plugin lifecycle and watcher system as first-class concepts.

### Plugin Lifecycle

```
Binary launch          gRPC connect          Initialize RPC         Health poll (10s)
     |                      |                      |                      |
  process starts        Metadata()           Initialize()            Health()
  binds port            validates name       plugin starts work      monitors liveness
  prints QNTX_PLUGIN_PORT                    returns watchers,       restarts on 2
                                             routes, handlers        consecutive failures
```

#### Boot sequence

1. QNTX launches the plugin binary and waits for it to bind a port
2. gRPC connection established, `Metadata()` called to validate plugin identity
3. `Initialize(InitializeRequest)` sent with config, ATS endpoint, auth token
4. Plugin returns `InitializeResponse` with watchers, routes, handlers, schedules
5. QNTX registers watchers in DB, sets up HTTP proxy routes, registers async handlers
6. Health polling begins (every 10s)

#### Initialize contract

- **Called once per proxy lifetime.** A fresh process gets one `Initialize` call. The `initOnce` guard ensures HTTP routing lazy-init doesn't trigger a second call.
- **Re-initialization** (`ReinitializePlugin` / `ForceInitialize`) is only for config updates on an existing proxy — not for new processes after restart.
- **Plugins must handle re-init gracefully.** Stop previous state (nodes, connections, goroutines) before starting new ones. The `Initialize` handler may be called on a proxy that already has running state from a previous call.
- **Config comes from `am.toml`.** The `config` map in `InitializeRequest` contains key-value pairs from the plugin's config section.

#### Restart

Restart = disable (best-effort) + enable. There is no special restart path.

- Disable: gRPC `Shutdown()`, kill process, prune watchers, unregister handlers
- Enable: discover binary, launch process, connect gRPC, `Initialize`, register everything

This means a restart always produces a new process, new gRPC connection, new proxy with fresh `initOnce`.

#### Health polling

- Every 10 seconds, QNTX calls `Health()` on each running plugin
- 2 consecutive failures trigger automatic restart with exponential backoff (10s, 20s, 40s, ... capped at 640s)
- A single transient failure is tolerated
- Health resets on success

#### Shutdown

- QNTX sends `Shutdown()` RPC (best-effort, 5s timeout)
- Plugin process is killed
- Watchers pruned from DB, handlers unregistered

### Watcher Lifecycle

```
Plugin                          QNTX Core                        Watcher Engine
  |                                |                                  |
  |-- InitializeResponse -------->|                                  |
  |   (watchers: [...])           |                                  |
  |                               |-- SetupPluginWatchers() -------->|
  |                               |   (write to DB, idempotent)      |
  |                               |                                  |
  |                               |-- ReloadWatchers() ------------>|
  |                               |   (load from DB into memory)     |
  |                               |                                  |
  |                               |   attestation arrives            |
  |                               |                                  |
  |                               |   <-- predicate match --------  |
  |                               |                                  |
  |<-- ExecuteJob(attestation) ---|                                  |
  |   (handler_name routes it)    |                                  |
  |                                                                  |
```

### WatcherRegistration fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Unique suffix. Core prefixes with plugin name: `{plugin}-{id}` |
| `handler_name` | yes | Which `ExecuteJob` handler receives the match |
| `predicates` | yes | Attestation predicates to match (exact match) |
| `contexts` | no | Additional context filters |
| `max_fires_per_second` | no | Rate limit. 0 = no rate limiting. Default: 0 |

### Predicate matching

- A watcher fires when an attestation's predicates contain **any** of the watcher's predicates (OR semantics)
- Match is exact string equality, not prefix
- Rate limiting is per-watcher, not per-predicate

### Hot-swap behavior

Watchers survive plugin restart. On every `Initialize`:
- `SetupPluginWatchers` calls `CreateOrReplace` for each declared watcher
- Stale watchers (previously declared, no longer in the list) are pruned
- `ReloadWatchers` refreshes the engine's in-memory state
- This works identically on cold start, hot-swap enable, and crash recovery

### WebSocket keepalive contract

QNTX sends PING messages on the gRPC stream and expects PONG responses. This tells the plugin whether a browser client is still connected.

Plugins must read the incoming gRPC stream and reply to PING with PONG (echo the timestamp). Spawn a reader task or thread that checks the message type and responds accordingly.

Failure to respond causes QNTX to log `WebSocket pong timeout`. The keepalive interval and timeout are configurable in `am.toml` under `[plugin.websocket.keepalive]`:

```toml
[plugin.websocket.keepalive]
ping_interval_secs = 30
pong_timeout_secs = 60
```

### Error flow

When things go wrong, QNTX emits:

| Log message | Meaning | Plugin action |
|-------------|---------|---------------|
| `Failed to parse AX query for watcher` | Watcher predicate is malformed | Fix the predicate string in `WatcherRegistration` |
| `gRPC ExecuteJob failed` | Plugin returned an error from `ExecuteJob` | Check plugin-side handler logic |
| `Max retries exceeded, giving up` | `ExecuteJob` failed repeatedly | Check plugin health, logs |
| `WebSocket pong timeout` | Plugin ignores incoming WebSocket stream | Read the incoming stream and reply to PING with PONG |
| `Failed to setup plugin watchers` | DB write failed during `Initialize` | Check DB connectivity |

## Consequences

### Positive

- Plugin developers have a single reference for watcher behavior
- WebSocket keepalive requirement is discoverable before hitting the error
- Error messages can be cross-referenced against this document

### Negative

- Another document to maintain as the watcher system evolves

## Related

- [ADR-003: Plugin Communication](ADR-003-plugin-communication.md) — watchers are the reactive complement to attestation polling
- [ADR-004: Plugin-Pulse Integration](ADR-004-plugin-pulse-integration.md) — Phase 5 introduced watchers
- [Plugin Hot-Swap](../plugin-hot-swap.md) — watcher registration during enable/disable
- [gRPC Plugin API](../api/grpc-plugin.md) — `WatcherRegistration` proto docs
- [WebSocket API](../api/websocket.md) — WebSocket message types
