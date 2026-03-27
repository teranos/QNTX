# Handover: Rust/kern LSP & Temporal Pipeline

Branch: `claude/review-weekly-activity-1BoEq` (4 commits, +1558 lines across 15 files)

## What was done

### 1. Temporal resolution ported to Rust (`5000aba`)

The parser pipeline had a "temporal gap" — Rust parsed the grammar but Go resolved date expressions like "yesterday" or "3 days ago", requiring two boundary crossings (Rust→Go→resolve→back). Now it's a single WASM call.

**`crates/qntx-core/src/temporal.rs`** (~400 lines, 30+ tests)
- `resolve_temporal_expr(expr, now_ms) → Result<i64, String>` — pure computation, no system clock
- Handles: natural language (now/yesterday/tomorrow/last week), relative (3 days ago, in 2 weeks), named days (last friday), ISO dates, US dates
- `ResolvedTemporal` enum with tagged JSON: `Since{since_ms}`, `Until{until_ms}`, `On{start_ms,end_ms}`, `Between{start_ms,end_ms}`, `Over{value,unit}`

**`crates/qntx-wasm/src/lib.rs`** — `parse_ax_query_resolved` WASM export combines parse + resolve in one call

**`ats/parser/dispatch_qntxwasm.go`** — Go side now calls `ParseAxQueryResolved` passing `time.Now().UnixMilli()`, receives absolute timestamps

**`crates/qntx-core/src/parser/ast.rs`** — Extended `DurationUnit` with Hours, Minutes, Seconds variants

### 2. UI TODOs for rich editor migration (`d886bb5`)

Added TODOs in three files for future migration from plain `<input>` to a lightweight rich editor that can render inline semantic token coloring and temporal resolution badges:

- `web/ts/components/glyph/ax-glyph.ts:79` — primary TODO
- `web/ts/components/glyph/semantic-glyph.ts:89` — cross-reference
- `web/ts/system-drawer.ts:248` — cross-reference (space-triggered text input)

### 3. LSP Layer 1: semantic token classification (`3949878`)

Pure computation layer — classifies every token in an AX query by grammatical role using a state machine that mirrors the parser without building an AST. Never fails, suitable for real-time editor feedback on incomplete queries.

**Rust: `crates/qntx-core/src/semantic.rs`** (~200 lines, 10 tests)
- `classify_tokens(input) → Vec<SemanticToken>` with type indices matching LSP legend (0=keyword, 1=subject, 2=predicate, 3=context, 4=actor, 5=temporal, 6=operator, 7=string, 8=url, 9=unknown)
- `encode_lsp_tokens()` — delta-encoded 5-tuple array for LSP protocol
- WASM exports for both browser (`wasm-bindgen`) and wazero (raw memory ABI)

**kern: `qntx-plugins/kern/lib/classify.ml`** (~140 lines)
- Same architecture — sedlex lexer with position tracking + parser state machine
- JSON output matches Rust format; `encode_lsp` produces LSP delta format
- Wired into `plugin.ml` as `classify_query`; version bumped to 0.3.0

### 4. Go LSP handler wired to Rust WASM classifier (`2ad58f5`)

**`ats/lsp/service.go`** — `classifyTokens` now tries Rust WASM first, falls back to Go:
- `classifyTokensWasm` calls `engine.ClassifySemanticTokens(query)` via wazero
- `classifyTokensGo` is the original Go state machine (unchanged logic)
- `wasmSemanticTypeMap` bridges Rust u32 indices → Go `SemanticTokenType`

**`ats/wasm/engine.go`** — `ClassifySemanticTokens` + `WasmSemanticToken` type

Also fixed 3 clippy warnings in `temporal.rs` and ran `cargo fmt`.

## Architecture after these changes

```
  Browser               Go Server              Rust (WASM)
  ───────               ─────────              ───────────
  ax-glyph.ts ──────→ lsp/service.go
    (plain <input>)       │
                          ├─ classifyTokensWasm ──→ classify_semantic_tokens()
                          │    (primary)               qntx-core::semantic
                          │
                          └─ classifyTokensGo
                               (fallback)

  browser WASM ─────────────────────────────→ classify_semantic_tokens()
    (wasm-bindgen)                               (same Rust code)

  Go parser ────────→ parse_ax_query_resolved ──→ Parser::parse() +
    (dispatch_qntxwasm)  (single WASM call)       temporal::resolve_clause()
```

Both browser and server share the same Rust classifier — single source of truth.

## Known issues / environment constraints

- `make test` fails due to DNS timeouts (`storage.googleapis.com` unreachable) — Go dependency downloads blocked. Not a code issue.
- `go test ./ats/lsp/` requires `LD_LIBRARY_PATH=target/release` for the `libqntx_sqlite.so` shared library
- Frontend tests: 230 failures are pre-existing (mock/DOM environment issues in test runner)
- `db/` package has a pre-existing failing test (`migration_errors_include_stack_traces`)

## What passes

- `cargo test -p qntx-core` — 165 tests + 7 doc-tests, all green
- `go test ./ats/parser/` — green
- `go test -tags "rustsqlite,qntxwasm" ./ats/lsp/` — green (with LD_LIBRARY_PATH)
- `go test -tags "rustsqlite,qntxwasm" ./ats/wasm/` — green (with LD_LIBRARY_PATH)
- `go vet ./ats/lsp/` — clean
- `cargo clippy -p qntx-core -p qntx-wasm` — clean

## Next steps (not started)

1. **Rich editor migration** — replace plain `<input>` in ax-glyph with a lightweight editor that can render inline semantic coloring. TODOs are in the code.

2. **LSP Layer 2 (data-dependent)** — completions and hover. The WASM classifier already handles Layer 1; completions require access to the SymbolIndex (SQLite data), which lives in Go. Options:
   - Keep completions in Go (current approach, works)
   - Move SymbolIndex to Rust via `qntx-sqlite` FFI (heavier lift)

3. **kern WASM path** — `wasm_of_ocaml` would let kern run in the browser. `classify.ml` is ready; needs the WASM build infrastructure.

4. **LSP Layer 3 (protocol glue)** — JSON-RPC/WebSocket adapter in `server/lsp_handler.go`. Stays in Go. Marked as sunset candidate (CodeMirror being replaced by canvas).

5. **Lexer URL handling** — The Rust lexer splits URLs at `/`, so `https://github.com` becomes multiple tokens. URL detection at the semantic level is blocked on lexer changes.
