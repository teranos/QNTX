# ats-sqlite

Native SQLite storage backend for QNTX attestations.

## Features

- **AttestationStore** - Full CRUD operations
- **QueryStore** - Filtering, aggregation, and statistics
- **BoundedStore** - Configurable quota enforcement (16/64/64 default)
- **FFI/CGO** - C-compatible interface for Go integration
- **Schema Compatibility** - Uses Go migration files as source of truth

## Usage

### Rust

```rust
use ats_sqlite::{SqliteStore, BoundedStore, StorageQuotas};
use ats::{AttestationBuilder, storage::{AttestationStore, QueryStore}};

// Basic usage
let mut store = SqliteStore::in_memory()?;
let attestation = AttestationBuilder::new()
    .id("AS-1")
    .subject("ALICE")
    .predicate("knows")
    .context("work")
    .build();
store.put(attestation)?;

// With quotas
let quotas = StorageQuotas::new(100, 256, 256);
let mut bounded = BoundedStore::in_memory_with_quotas(quotas)?;
```

### Go (via CGO)

```go
import "github.com/teranos/QNTX/ats/storage/sqlitecgo"

store, err := sqlitecgo.NewMemoryStore()
defer store.Close()

// Use via standard Go interfaces
```

See [FFI.md](./FFI.md) for CGO integration details.

## Architecture

```
┌─────────────────────┐
│   ats-sqlite       │
│  (Rust storage)     │
├─────────────────────┤
│ • AttestationStore  │ ← CRUD operations
│ • QueryStore        │ ← Filtering/aggregation
│ • BoundedStore      │ ← Quota enforcement
└──────────┬──────────┘
           │
           ├─► SQLite (via rusqlite)
           │
           └─► FFI (C ABI for CGO)
```

## Testing

```bash
# Rust tests
cargo test -p ats-sqlite

# With FFI
cargo test -p ats-sqlite --features ffi

# Go CGO tests
CGO_ENABLED=1 go test -tags rustsqlite ./ats/storage/sqlitecgo
```

## Build

```bash
# Standard library
cargo build -p ats-sqlite

# With FFI/CGO support
cargo build --release --features ffi -p ats-sqlite
```

## Status

- **Phase 1**: ✅ Basic CRUD (AttestationStore)
- **Phase 2**: ✅ Queries + Quotas (QueryStore, BoundedStore)
- **Phase 3**: 🚧 FFI/CGO bindings (proof of concept)
- **Future**: Cross-validation with Go databases, IndexedDB backend (#334)
