# qntx-ax-ext

SQLite loadable extension that brings AX query capability to any SQLite connection. A D `-betterC` binary writes attestations to SQLite, then queries them through this extension without needing the Go/Rust server stack.

## Build

```bash
cd crates/qntx-ax-ext && cargo build --release
# Output: target/release/libqntx_ax_ext.dylib (macOS) / .so (Linux)
```

Excluded from the workspace (`Cargo.toml` exclude list) to avoid `rusqlite` feature conflicts (`bundled` vs `loadable_extension`). Must be built independently.

## SQL Functions

| Function | Input | Output | DB Access |
|----------|-------|--------|-----------|
| `ax_parse(text)` | AX query string | `AxFilter` JSON | No |
| `ax_query(json)` | `AxFilter` JSON | `AxResult` JSON | Yes — queries `attestations` table |
| `ax(text)` | AX query string | `AxResult` JSON | Yes — parse + query combined |

## Entry Point

SQLite auto-derives entry points from filenames. For `libqntx_ax_ext`, it looks for `sqlite3_qntxaxext_init` but the actual symbol is `sqlite3_qntxax_init`. Always pass the entry point explicitly:

```sql
-- CLI
.load target/release/libqntx_ax_ext sqlite3_qntxax_init
```

```d
// D / C
sqlite3_load_extension(db, "libqntx_ax_ext.dylib", "sqlite3_qntxax_init", &errmsg);
```

## Usage

```sql
-- Parse a natural language query into a filter
SELECT ax_parse('ALICE is author of GitHub');
-- {"subjects":["ALICE"],"predicates":["author"],"contexts":["GitHub"],...}

-- Query with an explicit filter
SELECT ax_query('{"subjects":["ALICE"]}');

-- Combined parse + query
SELECT ax('ALICE is author of GitHub');
```

## Architecture

```
sqlite3_load_extension(db, "libqntx_ax_ext.dylib", "sqlite3_qntxax_init")
        |
        v
sqlite3_qntxax_init()          <- manual entrypoint, captures API pointer table
        |
        +-- registers ax_parse()   -> qntx_core::parser::Parser::parse
        +-- registers ax_query()   -> build_sql() + sqlite3ext_prepare_v2/step/column
        +-- registers ax()         -> parse_to_filter + execute_query
```

- **sqlite-loadable** (Alex Garcia, same author as sqlite-vec): Handles `EXTENSION_INIT2`, function registration, API pointer table setup.
- **qntx-core**: Parser (`Parser::parse`), types (`AxFilter`, `AxResult`, `Attestation`, `AxSummary`).
- **Raw FFI**: Query execution uses `ext::sqlite3ext_*` functions (prepare_v2, bind_text, step, column_text, finalize) through the extension API pointer table. No `rusqlite::Connection` — the extension operates on the host's `sqlite3*` handle directly.

### SQL query builder

`build_sql()` mirrors `qntx-sqlite/src/store.rs` `QueryStore::query`. Generates:

```sql
SELECT id, subjects, predicates, contexts, actors, timestamp, source,
       attributes, created_at, signature, signer_did
FROM attestations WHERE 1=1
  AND EXISTS (SELECT 1 FROM json_each(subjects) WHERE value IN (?, ?))
  AND EXISTS (SELECT 1 FROM json_each(predicates) WHERE value IN (?))
ORDER BY created_at DESC
```

Parameters are bound positionally via `sqlite3ext_bind_text` with `SQLITE_TRANSIENT`.

## D `-betterC` Integration

The extension is loaded from D via SQLite's C API, which `-betterC` can call directly since SQLite's API is pure C.

```d
// hook.d — -betterC attestation writer + ax query hook
extern(C) int main() @nogc nothrow {
    sqlite3* db;
    int rc = sqlite3_open("attestations.db", &db);
    if (rc != SQLITE_OK) return 1;

    // Load ax extension (explicit entry point required)
    sqlite3_enable_load_extension(db, 1);
    rc = sqlite3_load_extension(db, "libqntx_ax_ext.dylib", "sqlite3_qntxax_init", null);
    if (rc != SQLITE_OK) return 1;

    // Query via ax
    sqlite3_stmt* stmt;
    sqlite3_prepare_v2(db, "SELECT ax('is member of ACME')", -1, &stmt, null);
    if (sqlite3_step(stmt) == SQLITE_ROW) {
        const(char)* result = cast(const(char)*)sqlite3_column_text(stmt, 0);
    }
    sqlite3_finalize(stmt);
    sqlite3_close(db);
    return 0;
}
```

The extension is loaded at runtime by SQLite's `dlopen` — it does NOT need to be compiled with the D binary.

## Known Limitations

1. **No temporal parsing**: `ax_parse` doesn't populate `time_start`/`time_end` from temporal clauses (`since 2024-01-01`, `over 5y`). `ax_query` handles these fields correctly if passed via filter JSON.
2. **No classification or conflict detection** ([#655](https://github.com/teranos/QNTX/issues/655)): Returns `conflicts: []` always.
3. **No cartesian expansion** ([#656](https://github.com/teranos/QNTX/issues/656)): Returns raw attestations, not individual claims. An attestation with `subjects: ["ALICE", "BOB"]` and `predicates: ["member"]` comes back as one result, not two separate claims. Callers must decompose multi-valued fields themselves. Summary counts are per-attestation, not per-claim. The expansion logic exists in `qntx-core::expand` — needs wiring into the extension.
