# Bounded Storage Architecture

QNTX implements a **configurable bounded storage strategy** to prevent unbounded database growth. The system enforces limits, evicts oldest attestations when exceeded, and logs eviction events for observability.

## Storage Limits (16/64/64 Strategy)

- **16 attestations** per (actor, context) pair
- **64 contexts** per actor
- **64 actors** per entity (subject)

All limits configurable via `am.toml`. When exceeded, the oldest attestations for that constraint are deleted and the event is logged to `storage_events`.

## Self-Certifying ASIDs

> **For bulk ingestion, self-certifying ASIDs are required.** Without them, the 64-actor limit causes silent data loss once exceeded.

Using the same actor for all ingestion hits the 64-entity limit. Instead, use each attestation's own ASID as its actor — this bypasses the limit entirely.

```go
// Self-certifying: ASID vouches for itself
asid, _ := id.GenerateASID(entity.ID, "processed", context, "")
attestation := &types.As{
    ID:     asid,
    Actors: []string{asid},  // Self-certifying
    // ...
}
```

Shared actors are appropriate for authority-based claims (`"github@oauth"`) or source tracking, but be aware of the 64-actor limit.

## Configuration

```toml
[database.bounded_storage]
actor_context_limit = 16   # attestations per (actor, context) pair
actor_contexts_limit = 64  # contexts per actor
entity_actors_limit = 64   # actors per entity (subject)
```

Zero or negative values fallback to defaults (16/64/64).

## Observability

### Database Glyph

The `db` symbol palette command opens the database glyph showing:
- Storage statistics (total attestations, unique actors/subjects/contexts)
- Recent eviction events with timestamps and details

Evictions are broadcast in real-time via WebSocket (`storage_eviction`).

### Storage Events Table

All eviction events are logged to `storage_events` with event type, actor/context/entity, deletion count, limit value, and eviction details (JSON).

```bash
# Query enforcement events
qntx db stats --limit 20

# Or directly
sqlite3 ~/.qntx/db/sqlite.db \
  "SELECT event_type, COUNT(*) FROM storage_events GROUP BY event_type"
```

## Enforcement Flow

Enforcement runs through Rust's single `rusqlite` connection (avoids dual-driver SQLite corruption). Uses a **half-bound threshold**: triggers at `limit + limit/2`, evicts down to `limit`. For actor_context_limit=16, this means enforcement triggers at 24 and evicts 8 at once, amortizing cost.

```
RustBackedStore.CreateAttestation()
  → Rust FFI: INSERT attestation
  → Rust: enforce_limits()
      ├─ actor_context_limit: if count > 24, evict oldest down to 16
      ├─ actor_contexts_limit: if contexts > 96, evict least-used down to 64
      └─ entity_actors_limit: if actors > 96, evict least-recent down to 64
      Each path: load batch → distill into summary → delete originals → insert distill attestation
      (each logs to storage_events)
```

Evicted attestations are **distilled** into a compressed summary before deletion. See [ADR-020: Attestation Distillation](../adr/ADR-020-attestation-distillation.md) for the full design.

## See Also

- [ADR-020: Attestation Distillation](../adr/ADR-020-attestation-distillation.md)
- [Configuration System](config-system.md)
- [Pulse Architecture](pulse-async-ix.md)
