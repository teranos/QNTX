# ADR-018: Watcher Lifecycle and Plugin Developer Experience

**Status:** Accepted
**Date:** 2026-05-03
**Deciders:** QNTX Core Team
**Related:** ADR-003 (Plugin Communication), ADR-004 (Plugin-Pulse Integration)

## Context

Watchers are the reactive primitive in QNTX. A plugin declares watchers in `InitializeResponse`, core writes them to the database, and the watcher engine fires `ExecuteJob` when attestations match. This works, but the developer experience has gaps that cause silent failures and confusion.

### Problems observed

1. **WebSocket ping-pong is undocumented.** Plugins implementing `HandleWebSocket` must read the incoming gRPC stream and respond to PING messages with PONG. If they ignore the stream (natural first instinct), QNTX logs "WebSocket pong timeout" and the connection dies. Nothing in the proto, docs, or examples explains this requirement.

2. **Watcher lifecycle is implicit.** The full path — `InitializeResponse.watchers` -> `SetupPluginWatchers` writes to DB -> `ReloadWatchers` loads into engine -> attestation match -> `ExecuteJob` routes to plugin — is spread across `client.go`, `watchers.go`, `engine.go`, and `watcher_handlers.go`. A plugin developer sees only the proto field and `ExecuteJob`.

3. **Error messages don't guide.** When a watcher fails to fire, the errors reference internal concepts (queue entries, execution records) rather than telling the plugin developer what went wrong and how to fix it.

4. **Predicate matching rules are undocumented.** Watchers match on predicates, but the matching semantics (exact vs prefix, single vs multi-predicate, rate limiting via `max_fires_per_second`) are only discoverable by reading `engine.go`.

## Decision

Document the watcher lifecycle as a first-class concept. This ADR serves as the canonical reference for how watchers work end-to-end.

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

**Rust plugins:** Use `PluginWebSocket` from `qntx-grpc` — it handles PING/PONG automatically:

```rust
let (ws, data_rx, response_stream) = PluginWebSocket::new(request.into_inner());
// data_rx receives DATA from browser (PINGs handled transparently)
// ws.send(b"...") sends DATA to browser
Ok(Response::new(response_stream))
```

**C++ plugins:** Read the incoming stream in a separate thread and reply to PING with PONG (echo the timestamp).

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
| `WebSocket pong timeout` | Plugin ignores incoming WebSocket stream | Rust: use `PluginWebSocket` from `qntx-grpc`. C++: read stream, reply PING with PONG |
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
