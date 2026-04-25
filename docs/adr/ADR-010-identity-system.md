# ADR-010: QNTX Identity System — Vanity Subjects and Attestation System Unique IDs

Date: 2026-03-06
Status: Completed (steps 1-4, 6). Step 5 won't-do.

## Context

QNTX used `teranos/vanity-id` (Go, v0.3.0) for all ID generation — attestation IDs, subject names, job IDs. The library was imported in 25+ files. It worked, but it was a single Go module that couldn't run in the browser, and it conflated two fundamentally different concerns: human-readable names and unique attestation identity.

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

Both layers are implemented in the Rust crate `qntx-id`, maintained in this repository. The Go dependency on `teranos/vanity-id` has been retired (#793).

**Design principles:**

- **Review, don't transcribe.** Each function is reconsidered during migration. The acronym tables and heuristic bloat in vanity-id are not carried forward blindly.
- **Pure core, pluggable boundaries.** ID generation is pure computation. RNG and storage lookups stay at the caller.
- **No regex.** String methods only (CLAUDE.md).
- **Expose via WASM.** Both wazero (Go server) and browser (wasm-bindgen) targets, following the pattern established in ADR-005.

**Migration order** (each phase independently shippable):

1. **Core alphabet and normalization** — Done. Custom alphabet (Crockford-inspired, no 0/1), Unicode-to-ASCII, seed cleaning.
2. **ASUID generation** — Done. Prefix system (`AS`, `JB`, `PX`), SPC display segments, random suffix.
3. **Random ID generation** — Done (#793). For non-attestation uses (embedding IDs, run IDs).
4. **WASM bridge** — Done. Both wazero and browser targets.
5. **Vanity ID generation** — Won't do. Subject name derivation (name→handle) is not needed.
6. **Retire `teranos/vanity-id`** — Done (#793). Removed from `go.mod`, all callers migrated.

## Consequences

### Positive

- **Cross-platform.** Browser generates ASUIDs the same way as server — same Rust code via WASM.
- **Readable logs.** ASUIDs carry SPC hints — you see what an attestation is about without looking it up.
- **Clean separation.** Vanity IDs (names) and ASUIDs (identity) are no longer conflated in one library.
- **Single implementation.** Rust crate replaces external Go module, runs on all platforms.

### Negative

- **Migration cost.** 25+ Go files were updated across multiple PRs.

### Neutral

- **Performance.** ID generation is not a bottleneck. The motivation is portability and readability.
- **vanity-id retired.** The Go module served its purpose as prior art. The lessons carried forward; the code didn't.

## References

- `teranos/vanity-id` v0.3.0 — prior art
- ADR-005: WebAssembly Integration
- `server/nodedid/` — existing Node DID infrastructure
