# ADR-010: QNTX Identity System — Vanity Subjects and Content-Addressed Attestation IDs

Date: 2026-03-06
Status: Accepted

## Context

QNTX uses `teranos/vanity-id` (Go, v0.3.0) for all ID generation — attestation IDs, subject names, job IDs. The library is imported in 25+ files. It works, but it's a single Go module that can't run in the browser, and it conflates two fundamentally different concerns: human-readable names and unique attestation identity.

QNTX is offline-first (ADR-005). Nodes generate attestations independently and sync via Merkle trees. The identity system must work without coordination between peers.

## Decision

QNTX's identity system has three orthogonal layers, each with distinct properties:

| Layer | Purpose | Uniqueness | Mutability | Example |
|---|---|---|---|---|
| **Vanity ID** | Human-readable subject handle | Semi-unique (context disambiguates) | Immutable once assigned | `SARAH`, `SBVH`, `ACME` |
| **ASUID** | Attestation identity | Globally unique, content-addressed | Derived from content (deterministic) | `AS-SARAH-AUTHOR-GITHUB-7K4M` |
| **Node DID** | Signer identity | Globally unique (ed25519 keypair) | Generated once per node | `did:key:z6Mk...` |

Node DIDs already exist (`server/nodedid/`). This ADR defines the first two layers and commits to implementing them in Rust.

### Vanity IDs

Vanity IDs are human-readable handles for **subjects only**. They are names, not keys.

- Variable length, derived from human names or entity names
- No global uniqueness guarantee — two `SARAH` subjects can coexist; the attestation graph disambiguates
- Custom alphabet (Crockford-inspired, excludes 0/1 to avoid O/I confusion)
- Unicode normalization to ASCII, name particle filtering (van, von, de)
- No randomness, no collision resolution — if two subjects share a vanity ID, that's fine

Vanity IDs do not apply to predicates, contexts, or actors.

### ASUIDs (Attestation System Unique IDs)

ASUIDs are content-addressed identifiers for attestations. Same content produces the same ASUID on any node, offline or online.

**Structure:**

```
AS-SARAH-AUTHOR-GITHUB-7K4M3B9X
╰prefix╯╰─S──╯╰──P──╯╰──C──╯╰─hash suffix─╯
```

- **Prefix**: Two-letter domain indicator (`AS` attestation, `JB` job, `PX` pulse execution)
- **S, P, C segments**: Truncated vanity-style representations of subject, predicate, context — for log readability, not for uniqueness
- **Hash suffix**: Derived from the attestation's content hash — this is where uniqueness comes from
- Separators between segments for visual scanning in logs
- Display form may show fewer hash chars; full ASUID carries 8 for sufficient entropy

**Content addressing means:**

- Same attestation content (SPC + actor + timestamp) = same ASUID, everywhere
- No RNG dependency — works identically in WASM, native, any platform
- Natural dedup on sync: two nodes that independently create the same attestation produce the same ASUID, which the Merkle tree reconciles trivially
- `qntx-core::sync::content_hash` is the existing foundation; ASUID derives from it

### Implementation: Rust crate `qntx-id`

Both layers are implemented in a new Rust crate, maintained in this repository. The Go dependency on `teranos/vanity-id` is retired.

**Design principles:**

- **Review, don't transcribe.** Each function is reconsidered during migration. The acronym tables and heuristic bloat in vanity-id are not carried forward blindly.
- **Pure core, pluggable boundaries.** ID generation is pure computation. Storage lookups (reserved word checking) are trait parameters.
- **No regex.** String methods only (CLAUDE.md).
- **Expose via WASM.** Both wazero (Go server) and browser (wasm-bindgen) targets, following the pattern established in ADR-005.

**Migration order** (each phase independently shippable):

1. **Core alphabet and normalization** — Custom alphabet (Crockford-inspired, no 0/1), Unicode-to-ASCII, seed cleaning. Foundation for display segments in ASUIDs.
2. **ASUID generation** — Content-addressed, prefix system (`AS`, `JB`, `PX`), SPC display segments, hash-derived suffix. Replaces all `GenerateASID*` calls (~20 call sites). This is the critical path.
3. **Random ID generation** — For non-attestation uses (embedding IDs, run IDs) where content-addressing doesn't apply. Replaces `GenerateRandomID`.
4. **WASM bridge** — Expose to both wazero and browser targets as each piece lands, not as a separate phase.
5. **Vanity ID generation** — Subject name derivation (name→handle). Not actively used in the codebase today — build when the feature is needed, not before.
6. **Retire `teranos/vanity-id`** — Remove from `go.mod` once all callers are migrated.

## Consequences

### Positive

- **Offline-first identity.** Browser generates the same ASUIDs as server — no coordination needed.
- **Deterministic.** Content-addressed IDs eliminate an entire class of collision bugs.
- **Readable logs.** ASUIDs carry SPC hints — you see what an attestation is about without looking it up.
- **Clean separation.** Vanity IDs (names) and ASUIDs (identity) are no longer conflated in one library.
- **Single implementation.** Rust crate replaces external Go module, runs on all platforms.

### Negative

- **Migration cost.** 25+ Go files need updating, incrementally.
- **Content changes = new ASUID.** If an attestation is amended, it gets a new ID. This is intentional (attestations are immutable claims) but callers must understand it.

### Neutral

- **Performance.** ID generation is not a bottleneck. The motivation is correctness and portability.
- **vanity-id retirement.** The Go module served its purpose as prior art. The lessons carry forward; the code doesn't.

## References

- `teranos/vanity-id` v0.3.0 — prior art
- ADR-005: WebAssembly Integration
- `qntx-core::sync::content_hash` — existing content hash foundation
- `server/nodedid/` — existing Node DID infrastructure
