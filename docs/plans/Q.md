# Q: The Execution Kernel (currently Rust)

**Q is the kernel**: execution semantics, attestation model, time, storage contracts, parsing, deterministic transforms.

**Rust is an implementation detail** chosen for safety, performance, WASM reach, and FFI viability. The abstractions defined here outlive the language choice.

## Boundary Definition

### Inside Q (invariant)

These live in the kernel and define QNTX semantics:

- Attestation model and lifecycle
- Ax query parser and evaluation
- Storage traits and contracts
- Deterministic scoring primitives
- Time representation and ordering
- Conflict detection logic
- Execution scheduling primitives

### Outside Q (replaceable)

These consume Q but are not part of it:

- Server transport (HTTP, gRPC, WebSocket)
- UI rendering (web, desktop, mobile)
- Cloud deployment glue
- Ingestion adapters
- AI/LLM calls
- Plugin ecosystems
- Presentation formatting

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

### Consolidate into Q

Merge `qntx` and `qntx-core` into a single `q` crate:

```
crates/
├── q/              # The kernel
│   ├── types/      # Proto-generated types
│   ├── fuzzy/      # Fuzzy matching engine
│   ├── storage/    # Storage abstraction
│   ├── attestation/# Attestation model
│   ├── parser/     # Ax query parser
│   ├── plugin/     # Plugin scaffolding
│   └── proto/      # gRPC definitions
└── q-wasm/         # WASM bindings (uses q, not part of kernel)
```

**Rationale:**
- Single source of truth for kernel code
- Clear naming (`q` = the kernel, `q-wasm` = bindings layer)
- Types and domain logic together
- Consumers import one crate

### Proto-First Types

Schema boundary types are proto-first because Q must cross environments (server, browser, desktop, plugins) with minimal friction and maximal compatibility.

**Q owns behavior, proto owns the contract surface.**

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

Phase out `typegen` (Go → Rust) as proto coverage expands.

### Go as Distribution Host

Go is a distribution and orchestration host. It consumes Q through stable boundaries (FFI for hot paths, proto for contracts). Core abstractions live in Q.

Integration points:
1. **CGO/FFI** for hot paths (fuzzy matching) - already working
2. **Proto types** for data structures - migration path
3. **Eventually**: More kernel logic exposed via CGO

## Future Work (Out of Scope)

These are documented for future reference, not this PR:

- [ ] Rename `qntx-core` → `q`
- [ ] Merge `qntx` into `q`
- [ ] Rename `qntx-wasm` → `q-wasm`
- [ ] Define core types in `.proto` files
- [ ] Replace typegen with proto generation
- [ ] Storage backends: SQLite (native), IndexedDB (WASM)
- [ ] CGO wrapper for storage (Go server uses Q storage)
- [ ] Move workspace profiles to root Cargo.toml

## References

- `crates/qntx-core/src/storage/mod.rs` - Storage TODOs for backends
- `.github/workflows/rsgo.yml` - Hybrid Rust + Go CI pipeline
- `ats/ax/fuzzy-ax/` - CGO integration example
