# WASM Capability Matrix

What runs in the shared Rust core, and how each runtime reaches it.

The Rust is in `crates/`: `ats` (parsing, filters, resolution, watchers), `ats-id`, `ats-sign`, and the stores `ats-sqlite`, `ats-duckdb` and `ats-indexeddb`. It reaches a runtime by one of three bridges:

- **Browser:** wasm-bindgen exports in `crates/ats-wasm/src/browser.rs`, wrapped by `web/ts/ats-wasm.ts`. Signing goes through laye-p2p's wasm (`web/ts/laye.ts`).
- **Go, pure functions:** wazero, `crates/ats-wasm/src/lib.rs` loaded by `ats/wasm/engine.go`, behind the `qntxwasm` build tag.
- **Go, the stores:** cgo into `libats_sqlite` (`ats/storage/sqlitecgo`) and `libats_duckdb` (`ats/storage/duckdbcgo`).

## In Rust

| Capability | Rust | Browser | Node (Go) |
|---|---|---|---|
| ax query parsing, time resolved | `ats::ax_filter` | `axFilter`, used by ax-element and ts-element | wazero `ax_filter`, `resolve_temporal_expr` |
| `as` command parsing | `ats::parser::as_command` | not exported | wazero `parse_as_command` |
| Attestation store | `ats-indexeddb`; `ats-sqlite`, `ats-duckdb` | IndexedDB, used by ax-element and ts-element | cgo |
| What a store takes | `Attestation::writable` | every put to IndexedDB | every put, both backends; refused as region `unwritable` |
| Which attestations a filter names | `AxFilter::admits`, `AxFilter::select` | IndexedDB query | watchers; SQLite and DuckDB SQL hold the same rule (names meet ASCII case aside) |
| `over` | `ats::ax::over` | in `query_attestations` | in the resolved query below |
| Query resolution: aliases, cartesian expansion, grouping, classification, conflicts | `ats::ax::execute_with_aliases` | no | cgo `storage_query_resolved`, `read_conn_query_resolved`; a parquet node reads its SQLite landing file |
| Watcher matching | `ats::watcher::match_watchers` | no | wazero `match_watchers`, and nothing else: a build without `qntxwasm` matches no watcher |
| Identity (ASUID) | `ats-id` | `generateASUID`, used by ts-element | wazero `generate_asuid`, `generate_compact_asuid`, `generate_random_id` |
| Attestation signing | `ats-sign` | laye-p2p `sign_attestation`, used by ts-element | wazero `canonical_attestation`; `ats/signing/testdata` holds the vectors both are tested against |
| Bounded storage enforcement | `ats-sqlite` | no | cgo |
| Cosine similarity | `ats::similarity` | `cosineSimilarity`, wrapped in `local-semantic-search.ts`, which nothing imports | not called: `server/embeddings/clustering.go` has its own `cosineSimilarityF32` |

## Exported and not called

| Bridge | Exports |
|---|---|
| wazero | `parse_ax_query`, `watch_filter`, `classify_claims`, `expand_cartesian_claims`, `group_claims`, `dedup_source_ids`, `id_clean_seed`, `id_normalize_for_lookup` |
| browser | `classify_claims`, `generate_random_id`, `id_clean_seed`, `id_normalize_for_lookup` |

The server classifies through the cgo query path, not these.

## In Go

| Capability | Go package | Why it is there |
|---|---|---|
| Attribute schema | `ats/attrs/` | `reflect`: Rust has no runtime reflection, so struct-tag schemas need a redesign, not a translation |
| SO action payloads | `ats/so/` | Rust parses the `so` actions (`ax_filter` returns them); Go turns them into a job |
| SO prompt execution | `ats/so/actions/prompt/` | holds a `database/sql` handle, calls `ai/provider`, submits through `pulse/async` |
| Pulse scheduling | `pulse/` | job orchestration, goroutines, database-bound |

## Elsewhere

- **Embeddings** are a plugin's (ADR-017): the node asks an `EmbeddingService` over gRPC.
- **Sync:** there is no sync protocol and no Merkle tree. The browser pushes its queued attestations with `POST /api/attestations` (`web/ts/api/attestation-sync.ts`).
- **The browser's queries skip** alias expansion and classification: it holds no aliases, and `query_attestations` does not resolve.
