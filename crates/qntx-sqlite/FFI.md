# qntx-sqlite FFI Integration

CGO bindings for using Rust qntx-sqlite from Go.

## Status: Proof of Concept

The FFI layer is functional and demonstrates successful cross-language integration:

- ✅ C ABI exposed via `ffi.rs`
- ✅ C header file for CGO
- ✅ Go wrapper package compiles and links
- ✅ Library lifecycle (new/free) working
- ✅ Version string successfully retrieved from Rust
- ⚠️  JSON format adapter needed for full compatibility

## Architecture

```
Go Server (ats/storage)
    ↓
CGO Wrapper (ats/storage/sqlitecgo)
    ↓
C ABI (crates/qntx-sqlite/ffi.rs)
    ↓
Rust SqliteStore (qntx-sqlite)
    ↓
SQLite Database
```

## Building

### 1. Build Rust Library

```bash
cargo build --release --features ffi -p qntx-sqlite
```

This creates `target/release/libqntx_sqlite.dylib` (macOS) or `.so` (Linux).

### 2. Run Go Tests

```bash
CGO_ENABLED=1 go test -tags rustsqlite ./ats/storage/sqlitecgo -v
```

## Current Limitations

### JSON Format Mismatch

Go's `types.As` and Rust's `qntx_core::Attestation` have slight serialization differences:

**Timestamps:**
- Go: `time.Time` → RFC3339 string (`"2026-01-26T02:00:40Z"`)
- Rust: `i64` → Unix milliseconds (`1706230840000`)

**Empty Collections:**
- Go: `nil` for empty maps → JSON `null`
- Rust: empty `HashMap` → must be present, can be `{}`

### Solution Approaches

**Option 1: JSON Adapter in Go (Recommended)**
```go
// Convert Go types.As to Rust-compatible JSON before FFI call
func toRustJSON(as *types.As) ([]byte, error) {
    return json.Marshal(struct {
        ID         string                 `json:"id"`
        Subjects   []string               `json:"subjects"`
        Predicates []string               `json:"predicates"`
        Contexts   []string               `json:"contexts"`
        Actors     []string               `json:"actors"`
        Timestamp  int64                  `json:"timestamp"`  // Unix ms
        Source     string                 `json:"source"`
        Attributes map[string]interface{} `json:"attributes"`
        CreatedAt  int64                  `json:"created_at"` // Unix ms
    }{
        ID:         as.ID,
        Subjects:   as.Subjects,
        Predicates: as.Predicates,
        Contexts:   as.Contexts,
        Actors:     as.Actors,
        Timestamp:  as.Timestamp.UnixMilli(),
        Source:     as.Source,
        Attributes: ensureNotNil(as.Attributes),
        CreatedAt:  as.CreatedAt.UnixMilli(),
    })
}
```

**Option 2: Custom Serialization in Rust**
Add `#[serde(with = "...")]` attributes to handle Go's format.

**Option 3: Binary Protocol**
Skip JSON entirely, use direct struct marshaling via FFI.

## Test Results

```
TestRustStore_Lifecycle: ✅ PASS
  - Library loads successfully
  - Version: 0.1.0

TestRustStore_CreateAndGet: ❌ FAIL
  - Error: timestamp format mismatch

Other tests: ❌ FAIL
  - Error: null attributes (need empty map)
```

## Next Steps

1. Add JSON adapter layer in `sqlitecgo` package
2. Run full Go integration test suite against Rust backend
3. Performance benchmarks (Go SQLStore vs Rust SqliteStore)
4. Consider replacing `ats/storage/sql_store.go` with CGO wrapper

## Files

```
crates/qntx-sqlite/
  src/ffi.rs                      # Rust FFI implementation
  include/storage_ffi.h           # C header
  FFI.md                          # This document

ats/storage/sqlitecgo/
  storage_cgo.go                  # Go CGO wrapper
  storage_cgo_test.go             # Integration tests
```

## Vision

If fully integrated, qntx-sqlite could replace the entire Go `db` package and `ats/storage` SQL implementation, consolidating all storage logic in the Rust core.
