# QNTXrs: Rust as the Deep Core

This document describes the architectural direction for Rust in QNTX.

## Vision

**Rust is the deep core language.** Domain logic, type definitions, and performance-critical code live in Rust. Go remains important for server distribution, but consumes Rust rather than defining core abstractions.

**Proto becomes the type source of truth.** Rather than Go generating Rust types (current typegen), types are defined in `.proto` files and generated for both languages. This is industry-standard and we already use proto for plugins.

## Current State (January 2026)

### Crate Structure

```
crates/
├── qntx/           # Generated types, plugin scaffolding, proto
├── qntx-core/      # Domain logic: fuzzy, storage, attestation, parser
└── qntx-wasm/      # WASM bindings for browser
```

### What's Working

- [x] Cargo workspace at project root
- [x] `qntx` crate with types, plugin scaffolding, proto definitions
- [x] `qntx-core` with fuzzy matching engine (replaces Go implementation)
- [x] `qntx-core` storage layer abstraction (traits for SQLite, IndexedDB, memory)
- [x] `qntx-wasm` WASM bindings for browser
- [x] CGO integration: Go server uses Rust fuzzy-ax via FFI
- [x] Typegen outputs directly to `crates/qntx/src/types/`
- [x] `qntx-python` uses shared proto from `qntx::plugin::proto`
- [x] CI: `rsgo.yml` hybrid pipeline (Rust + Go tested together)

### Consumers

| Crate | Uses |
|-------|------|
| qntx-python | `qntx::plugin::proto`, `qntx::types` |
| src-tauri | `qntx::types` |
| ats/vidstream | `qntx::types` |
| fuzzy-ax (CGO) | `qntx_core::FuzzyEngine` |

## Aspired Direction

### Consolidate into QNTXrs

Merge `qntx` and `qntx-core` into a single `qntxrs` crate:

```
crates/
├── qntxrs/         # THE Rust crate
│   ├── types/      # Proto-generated types
│   ├── fuzzy/      # Fuzzy matching engine
│   ├── storage/    # Storage abstraction
│   ├── attestation/# Attestation model
│   ├── parser/     # Ax query parser
│   ├── plugin/     # Plugin scaffolding
│   └── proto/      # gRPC definitions
└── qntx-wasm/      # WASM bindings (uses qntxrs)
```

**Rationale:**
- Single source of truth for Rust code
- Clear naming (`qntxrs` = Rust implementation of QNTX)
- Types and domain logic together
- Consumers import one crate

### Proto-First Types

Phase out `typegen` (Go → Rust) in favor of proto definitions:

```
proto/
├── attestation.proto   # Core attestation types
├── ax.proto            # Query types
├── plugin.proto        # Plugin protocol (existing)
└── types.proto         # Shared primitives
```

Both Rust and Go generate from proto:
- Rust: `prost` + `tonic` (already used for plugins)
- Go: `protoc-gen-go` (standard)

**Rationale:**
- Industry standard for cross-language types
- Already using proto for plugins
- Rust becomes source of truth for behavior, proto for schema
- Eliminates custom typegen maintenance

### Go Server Integration

Go server consumes Rust via:
1. **CGO/FFI** for hot paths (fuzzy matching) - already working
2. **Proto types** for data structures - migration path
3. **Eventually**: More domain logic in Rust, exposed via CGO

## Future Work (Out of Scope)

These are documented for future reference, not this PR:

- [ ] Rename `qntx-core` → `qntxrs`
- [ ] Merge `qntx` into `qntxrs`
- [ ] Define core types in `.proto` files
- [ ] Replace typegen with proto generation
- [ ] Storage backends: SQLite (native), IndexedDB (WASM)
- [ ] CGO wrapper for storage (Go server uses Rust storage)
- [ ] Move workspace profiles to root Cargo.toml

## References

- `crates/qntx-core/src/storage/mod.rs` - Storage TODOs for backends
- `.github/workflows/rsgo.yml` - Hybrid Rust + Go CI pipeline
- `ats/ax/fuzzy-ax/` - CGO integration example
