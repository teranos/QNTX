# Bounded Storage Architecture

## Overview

QNTX implements a **configurable bounded storage strategy** to prevent unbounded database growth while maintaining attestation history. The system automatically enforces storage limits and provides observability through telemetry.

## Storage Limits (16/64/64 Strategy)

The default strategy enforces three complementary limits:

- **16 attestations** per (actor, context) pair
- **64 contexts** per actor
- **64 actors** per entity (subject)

All limits are configurable via `am.toml` (see Configuration section below).

### Why These Limits?

**Actor/Context Limit (16)**: Prevents spam from a single source claiming repeatedly about the same entity in the same context.

**Actor Contexts Limit (64)**: Prevents a single actor from proliferating across too many contexts.

**Entity Actors Limit (64)**: Prevents unbounded growth of actors making claims about a single entity.

### Enforcement Behavior

When a limit is exceeded:
1. The **oldest attestations** for that constraint are deleted
2. The deletion event is logged to the `storage_events` table with full context
3. The new attestation is created normally

This maintains a rolling window of recent attestations while preventing unbounded growth.

## Self-Certifying ASIDs

> **For bulk ingestion, self-certifying ASIDs are required, not optional.** Without them, the 64-actor limit will cause silent data loss once exceeded. There is currently no other workaround.

### The Problem

Using the same actor string (e.g., `"processor@system"`) for all ingestion operations creates a hard limit:

```go
// ❌ PROBLEMATIC: Shared actor hits 64-entity limit
for _, entity := range entities {
    attestation := &types.As{
        Subjects:   []string{entity.ID},
        Predicates: []string{"processed"},
        Actors:     []string{"processor@system"},  // Same actor for all!
    }
}
// After 64 entities, attestations for the 65th trigger deletion
```

### The Solution

**Use the attestation's own ASID as its actor** (self-certifying pattern):

```go
// ✅ CORRECT: Self-certifying ASIDs bypass the 64-actor limit
import "github.com/teranos/vanity-id"

for _, entity := range entities {
    // Generate ASID with empty actor seed
    asid, err := id.GenerateASID(
        entity.ID,      // subject
        "processed",    // predicate
        context,        // object
        "",             // empty actor seed
    )

    // Use ASID as its own actor (self-referential)
    attestation := &types.As{
        ID:         asid,
        Subjects:   []string{entity.ID},
        Predicates: []string{"processed"},
        Actors:     []string{asid},  // Self-certifying!
        // ...
    }
}
```

### Benefits of Self-Certifying ASIDs

1. **Bounded storage compliance** - Each attestation has unique actor, bypasses 64-actor limit
2. **Self-certifying** - Attestation vouches for itself, no external authority needed
3. **Perfect provenance** - ASID directly traces to the creating attestation
4. **Immutable attribution** - Actor IS the attestation, cannot be spoofed
5. **Temporal ordering** - ASIDs encode timestamps for chronological queries

### When NOT to Use Self-Certifying

Self-certifying ASIDs are the **default best practice**, but there are specific cases where shared actors are appropriate:

- **Testing bounded storage** - Need predictable actor counts to verify limits
- **Authority-based claims** - When actor identity matters (e.g., `"github@oauth"` for verified GitHub data)
- **Source tracking** - When grouping attestations by originating system

In these cases, be aware of the 64-actor limit and monitor enforcement via `qntx db stats`.

## Configuration

### Default Configuration

QNTX uses sensible defaults (16/64/64) that work for most use cases. No configuration required.

### Custom Configuration

Create or edit `~/.qntx/am.toml` (or use project-specific `am.toml`):

```toml
[database.bounded_storage]
actor_context_limit = 16  # attestations per (actor, context) pair
actor_contexts_limit = 64 # contexts per actor
entity_actors_limit = 64  # actors per entity (subject)
```

**Example: Higher limits for archival systems:**

```toml
[database.bounded_storage]
actor_context_limit = 100  # More history per actor/context
actor_contexts_limit = 200 # Allow more diverse contexts
entity_actors_limit = 200  # More actors can claim about entities
```

**Example: Stricter limits for constrained environments:**

```toml
[database.bounded_storage]
actor_context_limit = 8   # Minimal history
actor_contexts_limit = 32
entity_actors_limit = 32
```

For configuration system details, see [Configuration System](config-system.md).

### Validation

The system validates configuration on startup:
- Zero or negative values automatically fallback to defaults (16/64/64)
- Invalid configurations are logged but don't prevent startup

## Observability

### Storage Events Table

All enforcement events are logged to the `storage_events` table:

```sql
CREATE TABLE storage_events (
    id INTEGER PRIMARY KEY,
    event_type TEXT NOT NULL,        -- which limit was enforced
    actor TEXT,                       -- actor involved (may be null)
    context TEXT,                     -- context involved (may be null)
    entity TEXT,                      -- entity involved (may be null)
    deletions_count INTEGER NOT NULL, -- how many attestations deleted
    timestamp TEXT NOT NULL,          -- when enforcement happened
    created_at TEXT NOT NULL          -- database record time
);
```

### CLI Commands

**View recent enforcement events:**

```bash
# Show database stats with last 5 enforcement events
qntx db stats --limit 5

# Show more events
qntx db stats --limit 20
```

**Query enforcement events directly:**

```bash
# Recent actor_context_limit enforcement
sqlite3 ~/.qntx/db/sqlite.db \
  "SELECT * FROM storage_events
   WHERE event_type = 'actor_context_limit'
   ORDER BY created_at DESC
   LIMIT 10"

# Count enforcement events by type
sqlite3 ~/.qntx/db/sqlite.db \
  "SELECT event_type, COUNT(*) as count
   FROM storage_events
   GROUP BY event_type"
```

### Telemetry Fields

- **event_type**: `actor_context_limit` | `actor_contexts_limit` | `entity_actors_limit`
- **actor**: The actor that triggered enforcement (null for entity limits)
- **context**: The context involved (null for actor limits)
- **entity**: The entity/subject involved (null for context limits)
- **deletions_count**: Number of attestations deleted to enforce limit
- **timestamp**: When the enforcement happened (ISO 8601)
- **created_at**: When the event was logged to database

## Implementation Details

### Code Organization

Bounded storage implementation is split into focused files:

- **`ats/storage/bounded_store.go`** - Main interface and coordination
- **`ats/storage/bounded_store_config.go`** - Configuration structures and defaults
- **`ats/storage/bounded_store_enforcement.go`** - Limit enforcement logic
- **`ats/storage/bounded_store_telemetry.go`** - Event logging and observability

### Enforcement Flow

```
User creates attestation
    ↓
BoundedStore.CreateAttestation()
    ↓
SQLStore.CreateAttestation() [writes to DB]
    ↓
enforceActorContextLimit()
    ├─ Count attestations for (actor, context)
    ├─ If > limit: DELETE oldest
    └─ Log event to storage_events
    ↓
enforceActorContextsLimit()
    ├─ Count unique contexts for actor
    ├─ If > limit: DELETE oldest context's attestations
    └─ Log event to storage_events
    ↓
enforceEntityActorsLimit()
    ├─ Count unique actors for entity
    ├─ If > limit: DELETE oldest actor's attestations
    └─ Log event to storage_events
```

### SQL Queries

**Actor/Context Limit:**
```sql
-- Count attestations per (actor, context)
SELECT COUNT(*)
FROM attestations, json_each(actors) as a, json_each(contexts) as c
WHERE a.value = ? AND c.value = ?

-- Delete oldest when over limit
DELETE FROM attestations
WHERE id IN (
    SELECT id FROM attestations, json_each(actors) as a, json_each(contexts) as c
    WHERE a.value = ? AND c.value = ?
    ORDER BY timestamp ASC
    LIMIT ?
)
```

Similar queries exist for actor_contexts_limit and entity_actors_limit.

## Migration

### Database Migration

The `storage_events` table is created automatically via migration `010_create_storage_events_table.sql`:

```sql
CREATE TABLE IF NOT EXISTS storage_events (...);
CREATE INDEX IF NOT EXISTS idx_storage_events_created_at ON storage_events(created_at DESC);
-- ... other indexes
```

Migrations are idempotent and safe to run multiple times.

### From Unbounded to Bounded Storage

If migrating from an unbounded system:

1. **Backup your database** before enabling bounded storage
2. **Review current data** to understand actor/entity distribution
3. **Configure appropriate limits** based on your data patterns
4. **Monitor enforcement events** after enabling to tune limits

```bash
# Backup before migration
cp ~/.qntx/db/sqlite.db ~/.qntx/db/sqlite.db.backup

# Check current statistics
qntx db stats

# Configure limits in am.toml based on current data
# Start conservative, increase as needed

# Monitor enforcement after enabling
qntx db stats --limit 20
```

## Testing

### Test Scripts

Test bounded storage behavior using explicit actors:

```bash
# Test actor_context_limit (16 attestations max)
for i in {1..18}; do
  qntx as ALICE is status_$i of PROJECT by test@user
done
qntx db stats --limit 5
# Should show 2 deletions at attestation #17 and #18

# Test entity_actors_limit (64 actors max)
for i in {1..66}; do
  qntx as BOB is role of context_$i by actor_$i
done
qntx db stats --limit 5
# Should show deletions starting at actor #65
```

### Unit Tests

See `ats/storage/bounded_storage_integration_test.go` for comprehensive test coverage:

- `TestBoundedStorage_DeletesWhenExceeding16PerActorContext`
- `TestBoundedStorage_DoesNotDeleteDifferentContexts`
- `TestBoundedStorage_ExactDomainReproduction`

## Best Practices

### For Application Developers

1. **Default to self-certifying ASIDs** unless you have a specific reason not to
2. **Monitor enforcement events** in production via `qntx db stats`
3. **Configure limits based on data patterns**, not arbitrary numbers
4. **Test enforcement behavior** before deploying to production
5. **Document actor semantics** when using shared actors

### For Library Authors

1. **Provide configuration hooks** for bounded storage limits
2. **Log enforcement events** to help users tune their systems
3. **Document actor patterns** and their bounded storage implications
4. **Use self-certifying by default** in examples and documentation

### For System Administrators

1. **Monitor `storage_events` table** for unexpected enforcement patterns
2. **Tune limits based on telemetry**, not assumptions
3. **Plan for growth** - increase limits before hitting them frequently
4. **Backup databases regularly**, especially when tuning limits

## FAQ

**Q: What happens if I set limits to 0?**
A: Zero or negative limits automatically fallback to defaults (16/64/64). Bounded storage requires positive limits.

**Q: Can I disable bounded storage entirely?**
A: No. Bounded storage is fundamental to QNTX's architecture. Use very high limits if you need more space.

**Q: How do I know if my limits are too low?**
A: Monitor `qntx db stats` for frequent enforcement events. If you're seeing deletions constantly, increase limits.

**Q: Does enforcement happen synchronously?**
A: Yes. Limits are enforced immediately after creating each attestation. This ensures consistent state.

**Q: What if enforcement deletes important data?**
A: This is why self-certifying ASIDs are recommended - they bypass the 64-actor limit. If using shared actors, configure higher limits or use multiple actors.

## See Also

- [Configuration System](config-system.md) - Configuration architecture
- [Pulse Architecture](pulse-async-ix.md) - How Pulse uses bounded storage
- [Understanding QNTX](../understanding-qntx.md) - Core concepts overview
