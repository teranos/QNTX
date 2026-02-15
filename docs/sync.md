# Sync: Content-Addressed Attestation Reconciliation

> Introduced in [#478](https://github.com/teranos/QNTX/pull/478). See [vision/reticulum.md](vision/reticulum.md) for Reticulum integration direction.

Every QNTX instance maintains an in-memory Merkle tree of its attestation store. When two instances connect, they compare trees and exchange only what differs. No central server, no coordination — just two peers and a WebSocket.

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        Rust (qntx-core)                          │
│  ┌──────────────────┐  ┌─────────────────────────────────────┐   │
│  │  content.rs       │  │  merkle.rs                          │   │
│  │  SHA-256 of       │  │  BTreeMap<[u8;32], Group>           │   │
│  │  semantic fields  │  │  Insert / Remove / Diff / Root      │   │
│  │  → 64-char hex    │  │  Thread-local global tree           │   │
│  └──────────────────┘  └─────────────────────────────────────┘   │
└─────────────┬────────────────────────────┬───────────────────────┘
              │ JSON                        │ JSON
   ┌──────────▼──────────┐      ┌──────────▼──────────┐
   │  wazero (Go server) │      │  wasm-bindgen        │
   │  sync/tree_qntx.go  │      │  browser.rs          │
   │  raw memory ABI     │      │  native JS types     │
   └──────────┬──────────┘      └──────────┬──────────┘
              │                             │
   ┌──────────▼──────────┐      ┌──────────▼──────────┐
   │  Go sync package    │      │  Browser / Tauri     │
   │  peer.go            │      │  (future)            │
   │  observer.go        │      │                      │
   │  protocol.go        │      │                      │
   └──────────┬──────────┘      └─────────────────────┘
              │
   ┌──────────▼──────────┐
   │  server/             │
   │  sync_handler.go     │
   │  /ws/sync            │
   │  /api/sync           │
   │  /api/sync/status    │
   └─────────────────────┘
```

## Content Identity

Every attestation gets a SHA-256 digest of its semantic fields:

| Included | Excluded |
|----------|----------|
| subjects | id (ASID) |
| predicates | attributes |
| contexts | created_at |
| actors | |
| timestamp | |
| source | |

Fields are canonicalized (sorted, null-byte joined) so two nodes creating the same claim independently produce the same hash. This is the deduplication primitive: content identity, not storage identity.

**Implementation:** `crates/qntx-core/src/sync/content.rs`

## Merkle Tree

Attestations are grouped by `(actor, context)` pairs — mirroring bounded storage. Each group hashes its leaves into a group hash. All group hashes roll up into a root hash.

```
          Root Hash
         /    |    \
    Group₁  Group₂  Group₃
    /  \      |      /  \
  leaf leaf  leaf  leaf leaf
```

When two nodes have the same root hash, they're fully synced. When roots differ, comparing group hashes identifies which `(actor, context)` pairs diverge — then only those attestations transfer.

**Implementation:** `crates/qntx-core/src/sync/merkle.rs`

## Protocol

The reconciliation protocol is **symmetric** — both sides run the same state machine. Neither is server or client.

| Phase | Both send | Purpose |
|-------|-----------|---------|
| 1 | `sync_hello` (root hash) | Quick check — if roots match, done |
| 2 | `sync_group_hashes` | Exchange all group hash pairs |
| 3 | `sync_need` | Each side says which groups it wants |
| 4 | `sync_attestations` | Each side sends what the other asked for |
| 5 | `sync_done` | Stats: sent and received counts |

Rate-limited: max 100 groups and 1000 attestations per sync session.

**Implementation:** `sync/protocol.go`, `sync/peer.go`

## Conflict & Convergence

Attestations are append-only claims — not mutable state. This eliminates most "conflict" in the distributed systems sense.

Two nodes creating different attestations about the same subject isn't a conflict. It's two claims. After sync, both nodes have both attestations. The consumer decides how to interpret multiple claims — that's application logic, not sync logic.

The Merkle tree is a set (`BTreeSet<[u8; 32]>`). Sets under union are commutative and idempotent:

- A ∪ B = B ∪ A — sync order doesn't matter
- A ∪ A = A — syncing twice changes nothing

Any sync topology converges. No vector clocks, no causal tracking.

### Revocations

Retraction is itself an attestation — "user-1's membership in team-eng is revoked, attested by hr-system." It propagates through sync like any other claim. The consumer sees both the original and its revocation and acts accordingly. The Merkle tree doesn't distinguish between claims and revocations — both are content-hashed leaves.

## Trust & Verification

The reconciliation protocol transfers attestations. It doesn't ask whether you *should* accept them. Trust is a layer above sync.

**Current model: accept everything.** Useful for personal multi-device sync where both sides are you. Trust follows from the explicit decision to add a peer.

**Future models** (see [vision/reticulum.md](vision/reticulum.md)):

- **TOFU**: Accept an actor's attestations on first contact, expect consistency after. A QNTX variant: TOFU at the (actor, context) level.
- **Actor signatures (Ed25519)**: Attestations carry a cryptographic signature. Self-authenticating — can propagate through untrusted intermediaries.
- **Context-scoped trust**: Accept attestations from an actor in some contexts but not others. The Merkle tree already groups by (actor, context) — during the Need phase, a node can decline to request groups it doesn't trust.

## Topology

### Manual (implemented)

Explicit peer list in `am.toml`. Sync on demand via the sync glyph's per-peer button, or `POST /api/sync`.

### Scheduled (implemented)

Same peer list, automated on `interval_seconds`. If a peer is offline during one cycle, the next catches up — the protocol is stateless between reconciliations. Live peer reachability (green/red dots) is broadcast to connected browsers.

### Reactive (next)

Persistent connections. `TreeObserver.OnAttestationCreated()` fires on every insert — adding "notify connected peers" triggers immediate reconciliation. Changes propagate within seconds instead of waiting for the next tick.

### Interest-based & gossip (future)

Context-based clustering and epidemic dissemination over Reticulum. See [vision/reticulum.md](vision/reticulum.md).

## Server Integration

### Startup

On server boot (`server/init.go:setupSync`):
1. Creates a `SyncTree` backed by WASM (or skips if WASM unavailable)
2. Creates a `TreeObserver` and registers it with the storage layer
3. Kicks off background backfill — loads all existing attestations into the tree

The observer is called asynchronously on every attestation creation, keeping the tree in sync with the store.

### Endpoints

**`/ws/sync`** — WebSocket endpoint for incoming peer connections. A remote QNTX instance connects here and both sides run `Peer.Reconcile()`.

**`POST /api/sync`** — Initiate outbound sync with a peer.

```json
POST /api/sync
{"peer": "https://phone.local:877"}
```

Response:
```json
{"sent": 12, "received": 3}
```

**`GET /api/sync/status`** — Current tree state.

```json
{"available": true, "root": "a1b2c3...", "groups": 42}
```

### Configuration

```toml
# am.toml
[sync]
interval_seconds = 300  # reconcile every 5 minutes (0 = manual only)

[sync.peers]
phone = "https://phone.local:877"
lab-server = "https://lab.university.edu:877"
```

## Files

| File | Role |
|------|------|
| `crates/qntx-core/src/sync/content.rs` | SHA-256 content hashing (Rust) |
| `crates/qntx-core/src/sync/merkle.rs` | Merkle tree with BTreeMap (Rust) |
| `crates/qntx-core/src/sync/mod.rs` | Module exports |
| `crates/qntx-wasm/src/lib.rs` | Wazero exports (8 sync functions) |
| `crates/qntx-wasm/src/browser.rs` | wasm-bindgen exports (8 sync functions) |
| `sync/tree.go` | `SyncTree` interface |
| `sync/tree_qntx.go` | WASM-backed implementation (build tag: `qntxwasm`) |
| `sync/tree_noqntx.go` | Panic stub without WASM |
| `sync/observer.go` | `TreeObserver` — hooks storage layer to Merkle tree |
| `sync/peer.go` | Symmetric reconciliation state machine |
| `sync/protocol.go` | Wire message types |
| `sync/peer_test.go` | Protocol tests with in-memory mocks |
| `sync/observer_test.go` | Observer tests |
| `server/sync_handler.go` | HTTP/WebSocket handlers, scheduled sync, peer status broadcast |
| `server/init.go` | `setupSync()` — tree, observer, backfill, sync ticker |
| `am/am.go` | `SyncConfig` — peer list, interval |

## Testing

**Rust** (113 tests including sync): `cargo test --package qntx-core`
- Content hash determinism, order independence, field exclusion
- Merkle tree insert/remove/diff/contains
- JSON API roundtrips

**Go** (11 sync tests): `go test ./sync/`
- Full protocol exchange with `chanConn` (channel-based mock)
- Already-in-sync, one-side-has-more, both-have-unique, empty trees
- Context cancellation, wire format roundtrip, timestamp serialization
- Observer insertion, multi-actor/context groups, nil safety

## Design Decisions

**All crypto in Rust.** Go never computes hashes — it passes JSON to WASM and gets hex strings back. One implementation, three runtimes (server via wazero, browser via wasm-bindgen, native tests via cargo).

**BTreeMap for determinism.** The Merkle tree uses Rust's BTreeMap/BTreeSet — sorted iteration without explicit sorting. Deterministic across platforms.

**Thread-local global tree.** The WASM module maintains a single tree instance per runtime in thread-local storage. Matches the existing FuzzyEngine pattern.

**Backfill on startup.** The tree starts empty and is populated from the store in a background goroutine. The observer catches attestations created during backfill. Root hash stabilizes within seconds of boot.

**Graceful degradation.** If WASM is unavailable (binary not built), `NewSyncTree()` panics and `setupSync()` recovers — the server runs without sync. All sync endpoints return 503.

## Budget Coordination

The `sync_done` message carries each peer's spend summary (daily/weekly/monthly) and cluster limit configuration. `CheckBudget()` aggregates local spend with non-stale peer spends before enforcing limits. See [architecture/budget-tracking.md](architecture/budget-tracking.md) for the two-tier enforcement model (node limits + cluster limits).

## Opportunities

Systems that currently operate per-node but could leverage sync infrastructure.

**Canvas sync.** Glyphs are positioned widgets with content — they map cleanly to `(actor, "canvas:" + id)` groups in the Merkle tree. Two nodes sharing a canvas co-create diagrams; mobile-to-desktop canvas sync falls out for free.

**Embedding sync.** Each node independently computes embeddings for semantic search. Syncing embeddings alongside attestations lets resource-constrained devices (phone) search against a desktop's index without re-computing. Requires extending the protocol to handle binary blobs.

**Distributed job scheduling.** Pulse jobs run on whichever node owns the DB. Peers could coordinate via leasing to avoid duplicate execution or enable failover when a node goes offline.

## Limitations

**No authentication.** The `/ws/sync` endpoint accepts any connection — any peer that can reach the port can pull all attestations. Sufficient for personal multi-device sync on trusted networks.

**Poll-based, not reactive.** Sync runs on a timer (`interval_seconds`). New attestations aren't pushed to peers until the next tick. Reactive push — triggering reconciliation from `TreeObserver.OnAttestationCreated()` — is the next step.
