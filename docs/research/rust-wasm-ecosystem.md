# Rust WASM Ecosystem: Gaps and Recommendations

Date: 2026-02-18
Status: Research
Related: ADR-005 (WASM Integration)

## Current State

QNTX already uses: wasm-bindgen, wasm-bindgen-futures, wasm-pack, js-sys, web-sys, serde/serde_json, prost, thiserror, ahash, sha2, strsim.

Release profile: `opt-level = "s"`, `lto = true`, `strip = true`.

Module size: 89KB. Parse latency: ~25µs.

## Missing No-Brainers

### 1. `console_error_panic_hook`

Without this, browser WASM panics show `RuntimeError: unreachable` with no useful information. With it, panic messages and locations route to `console.error`. Zero runtime cost until a panic occurs.

**Where**: qntx-wasm (browser feature), qntx-indexeddb

```toml
console_error_panic_hook = "0.1"
```

```rust
// Called once at WASM init
console_error_panic_hook::set_once();
```

### 2. `wasm-bindgen-test`

Regular `cargo test` compiles to native x86/ARM, not wasm32. It cannot catch WASM-specific issues (memory layout, JS interop, IndexedDB behavior). `wasm-bindgen-test` runs tests in headless Chrome/Firefox/Node.js.

Critical for qntx-indexeddb which can only be tested in a real browser.

**Where**: dev-dependencies for qntx-wasm, qntx-indexeddb

```toml
[dev-dependencies]
wasm-bindgen-test = "0.3"
```

### 3. `wasm-opt` in build pipeline

`wasm-opt` applies WASM-specific optimizations that LLVM doesn't (binaryen passes, dead code elimination across WASM boundaries). Typical savings: 10-20% on top of cargo release profile.

89KB → estimated ~72-75KB. Free win for "mobile potato" target.

**Where**: Makefile `wasm` target, after cargo build

```makefile
wasm-opt -Os --strip-debug -o ats/wasm/qntx_core.wasm ats/wasm/qntx_core.wasm
```

## Strong Candidates

### 4. `tsify-next`

Derives TypeScript type definitions directly from Rust structs via `#[derive(Tsify)]`. Closes the Rust→TypeScript type gap the same way protobuf closes Go→TypeScript. The web/ts/qntx-wasm.ts wrapper currently defines these shapes manually.

### 5. `gloo` sub-crates (timers, net, worker)

Official Rust WASM Working Group wrappers over web-sys. Relevant for PWA/offline-first roadmap (fetch for sync, Web Workers for background processing, timers for scheduling). Cherry-pick sub-crates as needed.

### 6. `tracing-web`

Routes `tracing` spans/events to `console.log` / `performance.mark` in browser. Existing qntx-core instrumentation would work in browser WASM without conditional compilation.

## Watch List

### WASM Component Model (`wit-bindgen`)

Standardizes the WASM boundary protocol (currently manual `(ptr << 32) | len`). Would eliminate custom memory management in ats/wasm/engine.go. Blocked on: wazero Component Model support is experimental, browser support behind flags.

### `wasm-streams`

Bridges Rust `Stream`/`Sink` to Web Streams API. Relevant when storage migration (ADR-005 step 3) reaches browser and needs to stream large query results across the WASM boundary.
