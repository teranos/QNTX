# WASM Capability Matrix

What runs in the shared Rust WASM core, and how far each capability is wired.

The shared crate is `ats` / `ats-id`. From there a capability travels three more steps to reach a user: **browser.rs** (`#[wasm_bindgen]` exports) → **TS wrapper** (`web/ts/ats-wasm.ts`) → **UI wired** (used by a glyph or component).

## Fully wired (browser + server)

All four steps done:

- Query parsing
- Attestation CRUD — persists client-side via IndexedDB; used by ax-glyph, ts-glyph
- Identity (ASUID) — used by ts-glyph
- Cosine similarity

## In Rust, partially wired

Already in the Rust core; the remaining steps are what's missing.

| Capability | browser.rs | TS wrapper | UI wired |
|---|---|---|---|
| Classification | Yes | No | No |
| Merkle sync | Yes | No | No |
| Cartesian expansion | No | No | No |
| Claim grouping/dedup | No | No | No |

## Go-only (candidates for migration)

These are currently implemented in Go. Moving them to Rust would let the browser use them offline.

| Capability | Go package | Blocker | Browser benefit |
|---|---|---|---|
| Alias resolution | `ats/alias/` | none | Expand queries locally without server |
| Conflict detection | `ats/ax/conflicts.go` | none | Preview conflicts before submitting |
| SO action dispatch | `ats/so/` | none | Recognize "so prompt" / "so csv" before submitting |
| Attribute schema | `ats/attrs/` | `reflect` — Rust has no runtime reflection, so struct-tag schemas need a redesign, not a translation | Schema validation for glyph rendering |

## Server-only (not moving)

| Capability | Why |
|---|---|
| SO prompt execution (`ats/so/actions/prompt/`) | Holds a `database/sql` handle, calls `ai/provider`, submits through `pulse/async` |
| Pulse scheduling | Job orchestration, goroutines, database-bound |
| Embeddings | External model I/O, Rust FFI |
| Sync protocol | WebSocket-bound, budget/quota coordination |
