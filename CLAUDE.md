# QNTX LAW

**Read, don't infer:**

- Stick to what's explicitly stated in code and documentation
- Don't add features, explanations, or context that weren't requested
- If something is unclear, ask - don't assume or fill in gaps
- State only what you can directly verify
- When a task cannot be completed correctly, stop and explain the blocker rather than implementing workarounds

## Configuration (am package)

**Zero means zero:** `0` always means literal zero - no special "disabled" or "unlimited" semantics. `0` workers = no workers. `0` ticker interval = no ticking. For "unlimited", use a high value. For "use default", omit the field.

**For Claude**: Ensure sensible positive defaults in `am/defaults.go`. Validation rejects negative values and zero where it has no meaning.

## Development Workflow

The developer always uses `make dev` to start the development environment with hot-reloading for both backend (port 877) and frontend (port 8820). `make dev` builds the Go backend and runs the hot-reloading TypeScript frontend dev server.

**KNOW** the developer is always running the latest version of QNTX. It is **FORBIDDEN** to discuss or question whether the developer has run the latest version. If there is an issue, it is in the code, not with running the latest binary (`make dev` solves this) or configuration (QNTX works without configuration).

## Testing

**The AI agent MUST execute `make test` before claiming completion of any work. The cost is ~17 seconds.**

`make test` runs both backend (Go) and frontend (TypeScript) tests. See [web/TESTING.md](web/TESTING.md) for frontend testing patterns.

**It is FORBIDDEN to craft custom test commands.**

**Tests passing ≠ feature is correct.** Only manual verification by the developer confirms behavior matches intent.

**Prose encodes vision:** PR descriptions, commit messages, and code comments **MUST** capture intent and reasoning from the user's own words, not describe implementation. Code is easily regenerated; vision outlives code. Ask questions to extract and preserve the user's mental model _verbatim_ rather than generating descriptive summaries.

## Type Generation

**NEVER manually edit files in `types/generated/`.** Fix the generator in `typegen/` instead, then run `make types`. See [typegen.md](docs/typegen.md) for struct tags and troubleshooting.

## Glyphs

Glyphs ⧉  are the universal UI primitive. Currently defined in the `sym` package (will become `glyph`).

See [GLOSSARY.md](docs/GLOSSARY.md) for symbol definitions and [glyphs.md](docs/vision/glyphs.md) for the architectural vision.

## Go Development Standards

### WASM Integration

- **WASM module**: Run `make rust-wasm` to build qntx-core WASM module before building with `qntxwasm` tag
- **Never use `_wasm.go` suffix**: Go excludes these files unless `GOOS=wasm`. Use different naming like `_qntx.go`

### Code Quality

- **CRITICAL**: Include available context in errors, logs, and messages. If variables exist in scope (URLs, paths, IDs, status codes), reference them. If critical information isn't in scope, bring it into scope. Generic messages like "operation failed" or "task completed" are FORBIDDEN.
Use `github.com/teranos/QNTX/errors` for go (wraps cockroachdb/errors). Always wrap with context:

```go
if err := os.ReadFile(configPath); err != nil {
    return errors.Wrapf(err, "failed to read config from %s", configPath)
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
