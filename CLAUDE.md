# QNTX Development Guide

**Read, don't infer:**

- Stick to what's explicitly stated in code and documentation
- Don't add features, explanations, or context that weren't requested
- If something is unclear, ask - don't assume or fill in gaps
- State only what you can directly verify
- When a task cannot be completed correctly, stop and explain the blocker rather than implementing workarounds

## Configuration (am package)

**Zero values are real values:** `0` means zero, not disabled. `0` workers means zero workers, `0` rate limit means no rate limiting. To disable a feature, comment it out or omit it entirely. Don't abuse zero values as disable flags.

**For Claude**: Ensure sensible defaults in `am/defaults.go`. Zero values must have actual semantic meaning.

## Development Workflow

The developer always uses `make dev` to start the development environment with hot-reloading for both backend (port 877) and frontend (port 8820). `make dev` builds the Go backend and runs the hot-reloading TypeScript frontend dev server. That means the developer always run's the latest version of QNTX, given that they used `make dev`. NEVER have a discussion with the developer about having run the latest version or not, expect that the developer is always running the latest version of whatever and do not discuss or fight the developer on this, if there is an issue it is almost guaranteed the be an issue in the code, not with running the latest binary (`make dev` solves this) or configuration (QNTX should work without configuration)

**Prose encodes vision:** PR descriptions, commit messages, and code comments **MUST** capture intent and reasoning from the user's own words, not describe implementation. Code is easily regenerated; vision outlives code. Ask questions to extract and preserve the user's mental model _verbatim_ rather than generating descriptive summaries.

## Type Generation

**NEVER manually edit files in `types/generated/`.** Fix the generator in `code/typegen/` instead, then run `make types`.

## Segments

**Note:** These symbols are defined in the `sym` package for consistent use across QNTX. See [GLOSSARY.md](docs/GLOSSARY.md) for complete definitions.

## Go Development Standards

### Code Quality

- **Deterministic operations**: Use sorted map keys, consistent error patterns, predictable behavior

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
