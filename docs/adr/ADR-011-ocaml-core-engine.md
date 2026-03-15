# ADR-011: OCaml Core Engine (kern)

## Status
Proposed — parser proven (PR #688), layered architecture emerging

## Context

ATS has the semantics of a language but no formal grammar. Query, creation, and subscription are three separate code paths with ad-hoc string processing. The current Rust parser in `crates/qntx-core` is a hand-rolled state machine. The classifier and expander are structural transformations. These are language problems.

Separately, database operations are migrating from Go to Rust (PR #691+). Rust is not being replaced — it is expanding into the engine layer.

## Decision

kern (OCaml) is the innermost layer — parser, classifier, expander. Rust is the engine — database, enforcement, query execution. Go is the shell — HTTP, plugins, UI.

Parser via menhir, AST as algebraic types, classifier and expander as pattern matches. Build with dune.

### Server deployment

kern currently runs as a gRPC plugin (same model as loom). The gRPC path works but adds latency for what should be a function call. The end state depends on how kern integrates with the Rust engine layer — see "Database ownership" below.

### Browser deployment

See ADR-012.

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
3. **kern stops being a plugin** — it becomes a linked library (OCaml compiled to a C-compatible shared object, called into from Rust or Go). No gRPC overhead, direct access to everything. The Rust migration makes this more natural — kern links into the Rust layer rather than Go.

## Open questions

- How kern integrates with the Rust engine layer (gRPC plugin → linked library → ?)
- sync/merkle: Rust or stays in Go
- Migration path during transition
- When to decide on database ownership model (1 vs 3)
