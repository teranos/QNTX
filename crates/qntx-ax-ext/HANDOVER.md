# qntx-ax-ext — Handover Document

## What It Is

A SQLite loadable extension (`libqntx_ax_ext.so`) that brings AX query capability to any SQLite connection. Built for the hook use case: a D `-betterC` binary writes attestations to SQLite, then queries them through this extension without needing the Go/Rust server stack.

## Build

```bash
cd crates/qntx-ax-ext && cargo build --release
# Output: target/release/libqntx_ax_ext.so
```

Excluded from the workspace (`Cargo.toml` exclude list) to avoid `rusqlite` feature conflicts (`bundled` vs `loadable_extension`). Must be built independently.

## SQL Functions

| Function | Input | Output | DB Access |
|----------|-------|--------|-----------|
| `ax_parse(text)` | AX query string | `AxFilter` JSON | No |
| `ax_query(json)` | `AxFilter` JSON | `AxResult` JSON | Yes — queries `attestations` table |
| `ax(text)` | AX query string | `AxResult` JSON | Yes — parse + query combined |

## Architecture

```
sqlite3_load_extension(db, "libqntx_ax_ext.so")
        │
        ▼
sqlite3_qntxax_init()          ← #[sqlite_entrypoint] via sqlite-loadable
        │
        ├── registers ax_parse()   → qntx_core::parser::Parser::parse
        ├── registers ax_query()   → build_sql() + sqlite3ext_prepare_v2/step/column
        └── registers ax()         → parse_to_filter + execute_query
```

- **sqlite-loadable** (Alex Garcia, same author as sqlite-vec): Handles `EXTENSION_INIT2`, function registration, API pointer table setup.
- **qntx-core**: Parser (`Parser::parse`), types (`AxFilter`, `AxResult`, `Attestation`, `AxSummary`).
- **Raw FFI**: Query execution uses `ext::sqlite3ext_*` functions (prepare_v2, bind_text, step, column_text, finalize) through the extension API pointer table. No `rusqlite::Connection` — the extension operates on the host's `sqlite3*` handle directly.

### SQL query builder

`build_sql()` (lib.rs:210) mirrors `qntx-sqlite/src/store.rs` `QueryStore::query`. Generates:

```sql
SELECT id, subjects, predicates, contexts, actors, timestamp, source,
       attributes, created_at, signature, signer_did
FROM attestations WHERE 1=1
  AND EXISTS (SELECT 1 FROM json_each(subjects) WHERE value IN (?, ?))
  AND EXISTS (SELECT 1 FROM json_each(predicates) WHERE value IN (?))
ORDER BY created_at DESC
```

Parameters are bound positionally via `sqlite3ext_bind_text` with `SQLITE_TRANSIENT`.

## Manual Verification Checklist

The extension compiles but has **not been runtime-tested** (no `sqlite3` CLI was available in the build environment). These must be verified manually:

- [ ] **Extension loads**: `sqlite3` CLI `.load ./libqntx_ax_ext` succeeds without error
- [ ] **ax_parse returns valid JSON**: `SELECT ax_parse('ALICE is author of GitHub');` returns `{"subjects":["ALICE"],"predicates":["author"],"contexts":["GitHub"],...}`
- [ ] **ax_parse error handling**: `SELECT ax_parse('');` returns an error (empty query), not a crash
- [ ] **ax_query against real data**: Insert an attestation, then `SELECT ax_query('{"subjects":["ALICE"]}');` returns it
- [ ] **ax combined function**: `SELECT ax('ALICE is author');` parses and queries in one call
- [ ] **Empty result set**: Query with no matches returns `{"attestations":[],"conflicts":[],"summary":{"total_attestations":0,...}}`
- [ ] **JSON column parsing**: Verify `subjects`, `predicates`, `contexts`, `actors` round-trip correctly (stored as JSON arrays, deserialized back)
- [ ] **Timestamp parsing**: Verify RFC3339 timestamps (`2024-01-15T10:30:00Z`) are correctly converted to Unix ms in the result
- [ ] **Blob column (signature)**: Verify non-NULL signatures are returned correctly via `column_value`+`value_blob` path
- [ ] **SQLITE_TRANSIENT binding**: Confirm no use-after-free on parameter strings (the `param_cstrs` Vec keeps them alive through the step loop, and `SQLITE_TRANSIENT` tells SQLite to copy)
- [ ] **Concurrent access**: Extension reads from the same db the D hook writes to — verify WAL mode handles this without `SQLITE_BUSY`
- [ ] **Release build size**: `cargo build --release` produces a reasonably-sized `.so` (debug build is 29MB, release should be ~3-5MB)

## Known Limitations

1. **No temporal filtering**: `ax_parse` converts subjects/predicates/contexts/actors but does NOT convert temporal clauses (`since 2024-01-01`, `over 5y`) into `time_start`/`time_end`/`over_comparison` on the `AxFilter`. Temporal fields are always `null` in the output. The parser recognizes them — the conversion to absolute timestamps is unimplemented.

2. **No fuzzy matching**: `qntx-core` has a `FuzzyEngine` for approximate predicate/context matching. The extension does exact matching only. A query for `"author"` will not find attestations with predicate `"author_of"`.

3. **No classification**: `qntx-core::classify::SmartClassifier` groups claims and detects conflicts. The extension returns `conflicts: []` always.

4. **No cartesian expansion**: `qntx-core::expand::expand_cartesian` breaks multi-dimensional attestations into individual claims. The extension returns raw attestations as-is.

5. **No error messages from SQLite**: The `ext` module does not expose `sqlite3ext_errmsg`. On prepare/step failure, the extension reports the return code (`rc=N`) but not the SQLite error string.

6. **`std::mem::transmute` for SQLITE_TRANSIENT**: Line 163 uses `std::mem::transmute(-1isize)` for the destructor parameter. This is correct but flagged by some linters. The `sqlite-loadable` crate doesn't export a `SQLITE_TRANSIENT` constant.

7. **No `make` integration**: Building requires `cd crates/qntx-ax-ext && cargo build --release`. Not wired into the top-level `Makefile` since it's excluded from the workspace.

## D `-betterC` Hook Integration

The extension is loaded from D via SQLite's C API, which `-betterC` can call directly since SQLite's API is pure C.

### Loading the extension

```d
import sqlite3; // SQLite C bindings (already available in -betterC)

// After opening the database:
int rc = sqlite3_enable_load_extension(db, 1); // Enable extension loading
assert(rc == SQLITE_OK);

char* errmsg;
rc = sqlite3_load_extension(db, "libqntx_ax_ext.so", null, &errmsg);
if (rc != SQLITE_OK) {
    // errmsg contains the failure reason
    sqlite3_free(errmsg);
}
```

### Querying

```d
// Option A: Combined parse + query
sqlite3_stmt* stmt;
rc = sqlite3_prepare_v2(db,
    "SELECT ax('ALICE is author of GitHub')", -1, &stmt, null);
rc = sqlite3_step(stmt);
if (rc == SQLITE_ROW) {
    const(char)* json = cast(const(char)*)sqlite3_column_text(stmt, 0);
    // json contains the full AxResult JSON
}
sqlite3_finalize(stmt);

// Option B: Two-stage (parse separately, useful for caching filters)
rc = sqlite3_prepare_v2(db,
    "SELECT ax_query(?)", -1, &stmt, null);
sqlite3_bind_text(stmt, 1, filter_json.ptr, cast(int)filter_json.length, SQLITE_TRANSIENT);
```

### Build integration

The D hook binary needs `libqntx_ax_ext.so` at runtime. Options:

1. **Relative path**: Place `.so` next to the D binary, load with `"./libqntx_ax_ext.so"`
2. **Absolute path**: Point to the Rust build output directly
3. **LD_LIBRARY_PATH**: Add the extension directory to the library search path

The extension does NOT need to be compiled with the D binary — it's loaded at runtime by SQLite's `dlopen`. The D binary only needs SQLite's C headers (which it already has for `-betterC` database access).

### Minimal D hook skeleton

```d
// hook.d — -betterC attestation writer + ax query hook
extern(C) int main() @nogc nothrow {
    sqlite3* db;
    int rc = sqlite3_open("attestations.db", &db);
    if (rc != SQLITE_OK) return 1;

    // Load ax extension
    sqlite3_enable_load_extension(db, 1);
    rc = sqlite3_load_extension(db, "libqntx_ax_ext.so", null, null);
    if (rc != SQLITE_OK) return 1;

    // Write attestation (normal INSERT)
    // ...

    // Query via ax
    sqlite3_stmt* stmt;
    sqlite3_prepare_v2(db, "SELECT ax('is member of ACME')", -1, &stmt, null);
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        // Process JSON result
        const(char)* result = cast(const(char)*)sqlite3_column_text(stmt, 0);
    }
    sqlite3_finalize(stmt);
    sqlite3_close(db);
    return 0;
}
```
