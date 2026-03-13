# ADR-011: OCaml Replaces Rust as Core Engine Language

## Status
Proposed — parser proven, core migration not yet started

## Context

ATS has the semantics of a language but no formal grammar. Query, creation, and subscription are three separate code paths with ad-hoc string processing. The current Rust parser in `crates/qntx-core` is a hand-rolled state machine. The classifier and expander are structural transformations. These are language problems.

## Decision

Replace `crates/qntx-core` with OCaml. Parser via menhir, AST as algebraic types, classifier and expander as pattern matches. Build with dune.

### Deployment: dual-target from one codebase

- **Server**: native OCaml binary, gRPC plugin (same model as loom)
- **Browser**: wasm_of_ocaml compiles to WASM, runs in the browser's native WebAssembly engine (WasmGC supported in Chrome 119+, Firefox 122+, Safari 18.2+)

### Why not WASI + wazero

wasm_of_ocaml outputs WasmGC instructions. wazero does not support WasmGC and has no timeline to add it ([#1860](https://github.com/tetratelabs/wazero/issues/1860), deprioritized behind simpler Wasm 3.0 proposals). The native gRPC path avoids this entirely — the server runs a native binary, the browser runs browser-native WASM.

### Fuzzy matching: deprecated

Fuzzy matching (`qntxwasm` build tag, Rust WASM module) is deprecated. Semantic search supersedes it. Once fuzzy is removed, the Rust WASM dependency drops entirely — kern becomes the only parser and `qntxwasm` is no longer needed as a build tag. If fuzzy matching is ever needed again, it can be a plugin.

### Build tags: coexistence during transition

Dual build tags coexist: `kern` for parsing, `qntxwasm` for the fuzzy matcher (until deprecated). kern wins via build tag priority — `dispatch_kern.go` is `//go:build kern`, `dispatch_qntxwasm.go` is `//go:build qntxwasm && !kern`. Once fuzzy is removed, `qntxwasm` drops from the tag list entirely.

## Current state

kern only parses — it returns a JSON AST and the Go side does everything else (temporal resolution, case normalization, query execution). The "core engine" isn't in kern yet. Only the grammar is.

### Known gaps

- **No OCaml tests.** The parser has zero tests. The round-trip works but there's no regression coverage. Menhir can generate `.messages` files for exhaustive error reporting — not wired up.
- **Error messages are generic.** kern returns "parse error". The Rust parser returns things like "wildcard/special character is not supported". kern needs better error reporting from menhir.
- **Proto codegen for OCaml is manual.** `protoc --ocaml_out` is run by hand. There's no Makefile target or Nix derivation for regenerating the OCaml proto when `domain.proto` changes. Loom has the same problem — both have 3000-line checked-in generated files.
- **RegisterGlyphs warning on startup.** The framework calls RegisterGlyphs on every plugin. kern doesn't implement it, so it returns Unimplemented. Harmless but noisy.
- **kern is not a "system plugin."** It has to be manually added to `am.toml`. It should always be loaded. The plugin system doesn't have a concept of required/system plugins yet.

## Database ownership

The hard question. To move beyond parsing into classification and expansion, kern needs to read and write attestations. Three paths:

1. **ATSStoreService expands massively** — kern calls back into Go for every DB operation. More gRPC surface, more latency, but the plugin boundary stays clean. This is the incremental path.
2. **kern gets direct DB access** — breaks the plugin model. kern would need SQLite bindings in OCaml, or a shared DB connection. Worst of both worlds.
3. **kern stops being a plugin** — it becomes a linked library (OCaml compiled to a C-compatible shared object, called from Go via CGO). No gRPC overhead, direct access to everything. But then it's not a plugin anymore. This is the end state if kern truly replaces qntx-core.

## Open questions

- sync/merkle: OCaml or stays in Rust/Go
- Migration path during transition
- When to decide on database ownership model (1 vs 3)
