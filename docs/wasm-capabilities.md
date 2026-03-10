# WASM Capability Matrix

What runs in the shared Rust WASM core, and how far each capability is wired.

"Rust core" = shared crate (`qntx-core`, `qntx-id`). "browser.rs" = `#[wasm_bindgen]` exports. "TS wrapper" = `web/ts/qntx-wasm.ts`. "UI wired" = used by a glyph or component. Each column is a step in the pipeline — capabilities progress left to right.

## Fully wired (browser + server)

| Capability | Rust core | browser.rs | TS wrapper | UI wired |
|---|---|---|---|---|
| Query parsing | Yes | Yes | Yes | Yes |
| Attestation CRUD | Yes | Yes (IndexedDB) | Yes | Yes (ax-glyph, ts-glyph) |
| Fuzzy search | Yes | Yes | Yes | Yes |
| Identity (ASUID) | Yes | Yes | Yes | Yes (ts-glyph) |
| Rich text search | Yes | Yes | Yes | Yes |
| Cosine similarity | Yes | Yes | Yes | Yes |

## In Rust, partially wired

| Capability | Rust core | browser.rs | TS wrapper | UI wired |
|---|---|---|---|---|
| Classification | Yes | Yes | No | No |
| Merkle sync | Yes | Yes | No | No |
| Cartesian expansion | Yes | No | No | No |
| Claim grouping/dedup | Yes | No | No | No |

## Go-only (candidates for migration)

These are currently implemented in Go. Moving them to Rust would let the browser use them offline.

| Capability | Go package | Lines | Pure? | Browser benefit |
|---|---|---|---|---|
| Alias resolution | `ats/alias/` | 117 | Yes | Expand queries locally without server |
| Attribute schema | `ats/attrs/` | 402 | Yes | Schema validation for glyph rendering |
| Graph normalization | `graph/helpers.go` | 150 | Yes | Consistent node IDs in browser graph viz |
| Conflict detection | `ats/ax/conflicts.go` | ~300 | Yes | Preview conflicts before submitting |
| SO parsing | `ats/so/` | ~3,191 | Mostly | Preview "so prompt" / "so csv" actions |
| Entity resolution | `ats/ix/` | ~900 | Mostly | Dedup and match entities offline |

## Server-only (not moving)

| Capability | Why |
|---|---|
| Pulse scheduling | Job orchestration, goroutines, database-bound |
| Embeddings | External model I/O, Rust FFI |
| Sync protocol | WebSocket-bound, budget/quota coordination |
