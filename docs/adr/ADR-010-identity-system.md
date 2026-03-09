# ADR-010: QNTX Identity System — Vanity Subjects and Attestation System Unique IDs

Date: 2026-03-06
Status: Accepted

## Context

QNTX uses `teranos/vanity-id` (Go, v0.3.0) for all ID generation — attestation IDs, subject names, job IDs. The library is imported in 25+ files. It works, but it's a single Go module that can't run in the browser, and it conflates two fundamentally different concerns: human-readable names and unique attestation identity.

## Decision

QNTX's identity system has three orthogonal layers, each with distinct properties:

| Layer | Purpose | Uniqueness | Mutability | Example |
|---|---|---|---|---|
| **Vanity ID** | Human-readable subject handle | Semi-unique (context disambiguates) | Immutable once assigned | `SARAH`, `SBVH`, `ACME` |
| **ASUID** | Attestation identity | Unique (random suffix) | Generated once per attestation | `AS-SARAH-AUTHOR-GITHUB-7K4M` |
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

ASUIDs are unique identifiers for attestations with readable SPC segments for log scanning.

**Structure:**

```
AS-SARAH-AUTHOR-GITHUB-7K4M3B9X
╰prefix╯╰─S──╯╰──P──╯╰──C──╯╰──suffix──╯
```

- **Prefix**: Two-letter domain indicator (`AS` attestation, `JB` job, `PX` pulse execution)
- **S, P, C segments**: Truncated vanity-style representations of subject, predicate, context — for log readability, not for uniqueness
- **Suffix**: Random characters from QNTX alphabet — this is where uniqueness comes from
- Separators between segments for visual scanning in logs
- Display form may show fewer suffix chars (4 in logs); full ASUID carries 8 for sufficient entropy

**Randomness is caller-provided:**

- No RNG in the crate — Go uses `crypto/rand`, browser uses `crypto.getRandomValues`
- Same random bytes + same SPC = same ASUID (deterministic given inputs, for testability)
- The crate is pure computation — platform-specific entropy stays at the boundary

### Implementation: Rust crate `qntx-id`

Both layers are implemented in a new Rust crate, maintained in this repository. The Go dependency on `teranos/vanity-id` is retired.

**Design principles:**

- **Review, don't transcribe.** Each function is reconsidered during migration. The acronym tables and heuristic bloat in vanity-id are not carried forward blindly.
- **Pure core, pluggable boundaries.** ID generation is pure computation. RNG and storage lookups stay at the caller.
- **No regex.** String methods only (CLAUDE.md).
- **Expose via WASM.** Both wazero (Go server) and browser (wasm-bindgen) targets, following the pattern established in ADR-005.

**Migration order** (each phase independently shippable):

1. **Core alphabet and normalization** — Custom alphabet (Crockford-inspired, no 0/1), Unicode-to-ASCII, seed cleaning. Foundation for display segments in ASUIDs.
2. **ASUID generation** — Prefix system (`AS`, `JB`, `PX`), SPC display segments, random suffix. Replaces all `GenerateASID*` calls (~20 call sites). This is the critical path.
3. **Random ID generation** — For non-attestation uses (embedding IDs, run IDs). Replaces `GenerateRandomID`.
4. **WASM bridge** — Expose to both wazero and browser targets as each piece lands, not as a separate phase.
5. **Vanity ID generation** — Subject name derivation (name→handle). Not actively used in the codebase today — build when the feature is needed, not before.
6. **Retire `teranos/vanity-id`** — Remove from `go.mod` once all callers are migrated.

## Consequences

### Positive

- **Cross-platform.** Browser generates ASUIDs the same way as server — same Rust code via WASM.
- **Readable logs.** ASUIDs carry SPC hints — you see what an attestation is about without looking it up.
- **Clean separation.** Vanity IDs (names) and ASUIDs (identity) are no longer conflated in one library.
- **Single implementation.** Rust crate replaces external Go module, runs on all platforms.

### Negative

- **Migration cost.** 25+ Go files need updating, incrementally.

### Neutral

- **Performance.** ID generation is not a bottleneck. The motivation is portability and readability.
- **vanity-id retirement.** The Go module served its purpose as prior art. The lessons carry forward; the code doesn't.

## References

- `teranos/vanity-id` v0.3.0 — prior art
- ADR-005: WebAssembly Integration
- `server/nodedid/` — existing Node DID infrastructure
