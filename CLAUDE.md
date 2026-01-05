# QNTX Development Guide

## Working with this Project

**Read, don't infer:**
- Stick to what's explicitly stated in code and documentation
- Don't add features, explanations, or context that weren't requested
- If something is unclear, ask - don't assume or fill in gaps
- State only what you can directly verify

## Segments

QNTX uses segments across the project:

- **꩜** (Pulse) - Async operations, rate limiting, job processing
- **⌬** (by) - Actor/catalyst, origin of action
- **≡** (am) - Configuration and system settings
- **⨳** (ix) - Data ingestion/import
- **⋈** (ax) - Expand/query, contextual surfacing
- **⊔** (db) - Database/storage, material retention substrate

**Note:** These symbols are defined in the `sym` package for consistent use across QNTX.

**For Claude**: Use these segments consistently when referencing system components.

## Configuration (am package)

**Core Principle: QNTX works out of the box without configuration.**

The `am` package ("I am" - core being/state) manages all QNTX configuration:

- **Location**: `github.com/teranos/QNTX/am`
- **Philosophy**: Sensible defaults for immediate use; configuration optional for customization
- **Precedence**: System < User < Project < Environment Variables
- **File naming**: Prefers `am.toml` (new) but supports `config.toml` (backward compat)
- **Scope**: Core infrastructure only (Pulse, REPL, Server, Code, LocalInference, Ax, Database)

### Key Design Decisions

- **Zero values have meaning**: `0` workers = disabled, `0` rate limit = unlimited
- **Empty is valid**: Empty database path defaults to `qntx.db`
- **Multi-source merge**: All config files merge; later sources override earlier ones

**For Claude**: When adding config options, ensure sensible defaults exist in `am/defaults.go`. Only require configuration when truly necessary.

## Type Generation

**NEVER manually edit files in `types/generated/`.** Fix the generator in `code/typegen/` instead, then run `make types`.

## Go Development Standards

### Code Quality

- **Deterministic operations**: Use sorted map keys, consistent error patterns, predictable behavior

### Error Handling

Use `github.com/teranos/QNTX/errors` (wraps cockroachdb/errors). Always wrap with context:

```go
if err := doSomething(); err != nil {
    return errors.Wrap(err, "failed to do something")
}
```

See `errors/README.md` for full documentation.

### Testing

**CRITICAL: Database Testing Pattern**

NEVER create database schemas inline in tests. ALWAYS use the migration-based test helper.

**Correct Pattern:**
```go
import qntxtest "github.com/teranos/QNTX/internal/testing"

func TestSomething(t *testing.T) {
    db := qntxtest.CreateTestDB(t)  // Uses real migrations
    // ... test code
}
```

**Why:**
- `qntxtest.CreateTestDB(t)` runs actual migration files from `db/sqlite/migrations/`
- Ensures tests use identical schema to production
- Migrations are the single source of truth
- Auto-cleanup via `t.Cleanup()`

**NEVER do this:**
```go
// ❌ WRONG - Brittle, duplicates schema logic
db.Exec("CREATE TABLE attestations ...")
db.Exec("CREATE INDEX ...")
```

**Pattern used throughout:**
- `ats/storage/*_test.go` - All tests use `qntxtest.CreateTestDB(t)`
- `internal/testing/database.go` - Implementation using `db.Migrate()`
