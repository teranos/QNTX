# QNTX LAW

**Read, Verify, Proof. Don't infer, don't assume, don't theorize.**

- Stick to what's explicitly stated in code and documentation
- When a task cannot be completed correctly, stop and explain the blocker rather than implementing workarounds
- **Maximize signal-to-noise: essential information only, no filler.**

**Zero means zero:** `0` always means literal zero - no special "disabled" or "unlimited" semantics. `0` workers = no workers. `0` ticker interval = no ticking. For "unlimited", use a high value. For "use default", omit the field.

**Nil is nil:** nil, empty and zero mean nothing. Meaning given to nothing is banned outright: no granting all namespaces, no unrestricted, no every. Banned the same way: a discarded result, a catch-all branch, a fallback value, an error logged and carried on from, a config default. `internal/tools/nilcheck` (Go, Rust) and `web/eslint.config.js` (TypeScript) stop the build on a new one; what already stands is debt that only falls.

"it needs to be abolutely clear you cant attribute meaning to nil like granting all namepsaces"

A sigil handles a server capability, the reach table governs it with attestation DSL policy lines like this:
`REACH is '/i/standing' of ROOT SUPER TOKEN ATTESTOR PUBLIC_REGISTRATION`
`REACH is '/am/syscap' of ROOT`

**Symbols:** inside the system it knows only about the sg and the ui know's not the sg except for the one place its mapped to a glyph, which is the symbol.

"an sg is a segment like as is of by at am i"

## Testing

`make test` runs both backend (Go) and frontend (TypeScript) tests. See [web/TESTING.md](web/TESTING.md) for frontend testing patterns.

**Tests passing ≠ feature is correct.** Only manual verification by the developer confirms behavior matches intent.

**Prose encodes vision:** PR descriptions, commit messages, and code comments **MUST** capture intent and reasoning from the user's own words, literally, don't describe implementation details, User Vision outlives derived code. Ask questions to preserve the user's mental model _verbatim_ rather than descriptive interpreted summaries. **Maximize signal-to-noise: essential context only, no filler.**

## Regex

**FORBIDDEN.** Regex is banned in code. Use string methods (`split`, `indexOf`, `includes`, `startsWith`, `endsWith`, `slice`) instead.

## Identity

Who may log in, the provider ceremony, and what a passkey carries: [ADR-030](docs/adr/ADR-030-identity-providers.md). Access tokens: [ADR-025](docs/adr/ADR-025-access-tokens.md).

## Plugins

**ANY edit to a plugin MUST bump its version in `Metadata().Version`.** Plugins run as separate processes.

**Hot-swap:** Plugins are added, configured, enabled and disabled in the plugin element, at runtime. See [ADR-002](docs/adr/ADR-002-plugin-configuration.md).


1. `plugins_add` a repo or tree URL. Its last segment is the plugin's name, and equals `Metadata().Name`; the binary is `qntx-<name>-plugin`.
2. `PUT /api/plugins/<name>/config` writes the record: the plugin's own keys, `namespace` (ADR-046), and `build.core`, `build.command`, `build.output`, `build.packages`, `build.inputs`, `build.inputs_env` ([server/plugin_build.go](server/plugin_build.go)).
3. `plugins_enable`.
4. A push to a `build.*` source reaches the GitHub App's webhook; the node builds with its own nix, installs and restarts the plugin.

A plugin declares routes; QNTX makes each a sigil and an MCP tool. Go: `DeclaredRoutes()`.

A Go plugin imports `plugin/grpc`, which links `libats_sqlite`: build with `-tags rustsqlite`, after removing `target/release/libats_sqlite.so`, so the binary carries the static library.

TODO: sweep every repository and file for the old am.toml way of managing QNTX plugins, and point each at [ADR-002](docs/adr/ADR-002-plugin-configuration.md).

## Go Development Standards

### WASM Integration

- **WASM module**: Run `make ats` to build ats WASM module before building with `qntxwasm` tag
- **Never use `_wasm.go` suffix**: Go excludes these files unless `GOOS=wasm`. Use different naming like `_qntx.go`

### Code Quality

- **CRITICAL**: In errors, logs, and messages. If variables exist in scope (URLs, paths, IDs, status codes), reference them.
Use `errors "github.com/teranos/sacred-error"` for go, the one error package: the Sacred Error shape, and building and wrapping over cockroachdb/errors. Always wrap with context:

"but i want sacred-error to be the one"

```go
if err := os.ReadFile(configPath); err != nil {
    return errors.Wrapf(err, "failed to read config from %s", configPath)
}
```

See [github.com/teranos/sacred-error](https://github.com/teranos/sacred-error) and its `ERROR.md`.

### Testing

**CRITICAL: Database Testing Pattern**

ALWAYS use the migration-based test helper.

**Correct Pattern:**

```go
import qntxtest "github.com/teranos/QNTX/internal/testing"

func TestSomething(t *testing.T) {
    db := qntxtest.CreateTestDB(t)  // Uses real migrations
    // ... test code
}
```

**Two runners read that directory, and production is the Rust one.** Tests go through Go's `db.Migrate`, which walks the directory. Production goes through `ats-sqlite::migrate` via `sqlitecgo.NewFileStore` — on both backends, because passkeys and operational tables live in SQLite even when `backend = "parquet"`. Rust's list is generated by `crates/ats-sqlite/build.rs`, so adding a `.sql` file is the whole job.

**NEVER do this:**

```go
// ❌ WRONG - Brittle, duplicates schema logic
db.Exec("CREATE TABLE attestations ...")
db.Exec("CREATE INDEX ...")
```

**Pattern used throughout:**

- `ats/storage/*_test.go` - Tests use either `qntxtest.CreateTestDB(t)` or `testutil.SetupTestDB(t)` (both migration-based)
