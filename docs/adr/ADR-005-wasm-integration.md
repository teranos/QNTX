# ADR-005: WebAssembly Integration for Browser and Mobile Deployment

Date: 2026-02-01
Status: Accepted

## Context

QNTX has multiple Rust components integrated into a Go server via CGO. The vision includes running entirely in the browser (offline-first), on mobile, and as a Tauri desktop app — all sharing the same core logic.

## Decision

All shared computation moves to Rust crates compiled to WebAssembly, running on:
- **Go server**: wazero (pure Go, no CGO)
- **Browser**: wasm-bindgen (native WebAssembly API)
- **Tauri desktop**: embedded webview (same as browser) or native Rust (no WASM overhead)

### Architecture

```
┌─────────────┐     ┌──────────────┐     ┌────────────┐
│   Browser   │     │ Tauri Desktop│     │  Go Server │
│   (web/ts)  │     │  (src-tauri) │     │  (cmd/qntx)│
└──────┬──────┘     └──────┬───────┘     └─────┬──────┘
       │                   │                    │
       └───────────────────┼────────────────────┘
                           │
                    ┌──────▼──────┐
                    │  Rust WASM  │
                    │  (shared)   │
                    └─────────────┘
```

### Key design choices

- **Two WASM targets** from one codebase: wazero (raw memory ABI, `go:embed`) and browser (wasm-bindgen, IndexedDB storage)
- **JSON across the boundary**: strings cross as JSON, typed arrays for hot paths (cosine similarity)
- **Fallback path**: Go implementations maintained via `qntxwasm` / `!qntxwasm` build tags during migration
- **Pure computation**: platform-specific concerns (RNG, storage, networking) stay at the caller boundary

### What moves to WASM

Any logic that both browser and server need: parsing, fuzzy search, classification, sync, identity generation, expansion. See [wasm-capabilities.md](../wasm-capabilities.md) for the current capability matrix and migration candidates.

## Consequences

### Positive

- **Unified logic**: identical implementation across all platforms
- **Offline-first**: full functionality without server connection
- **Single source of truth**: Rust crate, not duplicated Go + TypeScript

### Negative

- **WASM overhead**: serialization boundary, slightly slower than native
- **Build complexity**: `make wasm` required before Go compilation
- **Debugging**: stack traces cross WASM boundary

### Neutral

- **wazero is the only option**: pure-Go constraint rules out wasmtime/wasmer (they use CGO)

## References

- ADR-010: Identity system (ASUID generation via WASM)
- wazero: https://wazero.io/
- [Issue #387](https://github.com/teranos/QNTX/issues/387): Parser design flaws exposed during WASM integration
