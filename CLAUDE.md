# QNTX Development Guide

## Working with this Project

**Read, don't infer:**
- Stick to what's explicitly stated in code and documentation
- Don't add features, explanations, or context that weren't requested
- If something is unclear, ask - don't assume or fill in gaps
- State only what you can directly verify

## Segments

QNTX uses segments across the project:

### Primary SEG Operators (UI/Command Symbols)
- **⍟** (i) - Self, your vantage point into QNTX
- **≡** (am) - Configuration and system settings
- **⨳** (ix) - Data ingestion/import
- **⋈** (ax) - Expand/query, contextual surfacing
- **⌬** (by) - Actor/catalyst, origin of action (all forms: creator, source, authenticated user)
- **✦** (at) - Temporal marker/moment
- **⟶** (so) - Therefore, consequent action

### System Symbols
- **꩜** (Pulse) - Async operations, rate limiting, job processing (always prefix Pulse-related logs)
- **✿** (PulseOpen) - Graceful startup
- **❀** (PulseClose) - Graceful shutdown
- **⊔** (db) - Database/storage layer

### Attestation Building Blocks
- **+** (as) - Assert, emit an attestation
- **=** (is) - Identity/equivalence in attestations
- **∈** (of) - Membership/belonging in attestations

**Note:** These symbols are defined in the `sym` package for consistent use across QNTX. See [GLOSSARY.md](docs/GLOSSARY.md) for complete definitions.

**For Claude**: Use these segments consistently when referencing system components.

## Configuration (am package)

**Core Principle: QNTX works out of the box without configuration.**

The `am` package ("I am" - core being/state) manages all QNTX configuration:

- **Location**: `github.com/teranos/QNTX/am`
- **Philosophy**: Sensible defaults for immediate use; configuration optional for customization
- **Precedence**: System < User < Project < Environment Variables
- **File naming**: Prefers `am.toml` (new) but supports `config.toml` (backward compat)
- **Scope**: Core infrastructure only (Pulse, Server, Code, LocalInference, Ax, Database)

### Key Design Decisions

- **Zero values have meaning**: `0` workers = disabled, `0` rate limit = unlimited
- **Empty is valid**: Empty database path defaults to `qntx.db`
- **Multi-source merge**: All config files merge; later sources override earlier ones

**For Claude**: When adding config options, ensure sensible defaults exist in `am/defaults.go`. Only require configuration when truly necessary.

## Development Workflow

Use `make dev` to start the development environment with hot-reloading for both backend (port 877) and frontend (port 8820).

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
