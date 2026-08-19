# ADR-010: QNTX Identity System — Vanity Subjects and Attestation System Unique IDs

Date: 2026-03-06
Status: Completed, except Vanity ID generation, which is won't-do.

## Context

QNTX used `teranos/vanity-id` (Go, v0.3.0) for all ID generation — attestation IDs, subject names, job IDs. The library was imported in 25+ files. It worked, but it was a single Go module that couldn't run in the browser, and it conflated two fundamentally different concerns: human-readable names and unique attestation identity.

## Decision

QNTX's identity system has four orthogonal layers, each with distinct properties:

| Layer | Purpose | Uniqueness | Mutability | Example |
|---|---|---|---|---|
| **Vanity ID** | Human-readable subject handle | Semi-unique (context disambiguates) | Immutable once assigned | `SARAH`, `SBVH`, `ACME` |
| **ASUID** | Attestation identity | Unique (random suffix) | Generated once per attestation | `AS-SARAH-AUTHOR-GITHUB-7K4M` |
| **Node DID** | Signer identity | Globally unique (ed25519 keypair) | Generated once per node | `did:key:z6Mk...` |
| **User DID** | The person | Globally unique (ed25519 keypair) | Derived from biometrics via WebAuthn PRF | `did:key:z6Mk...` |

A node was a server when this table was written. Since ADR-012 it is also a
browser, and `server/nodedid/` cannot reach one.

User DID was written here before anything derived one. It is real since
ADR-030's passkey gate: the browser asks the authenticator for a PRF output
and derives the key from it, so the same finger gives the same DID.

Of the three, only ASUID has a generator. Subjects carry names a human supplies,
checked by a write-time warning rather than derived. Node DID is minted in
`server/nodedid/`.

Node DIDs already exist (`server/nodedid/`). This ADR defines the first two layers and commits to implementing them in Rust.

The third layer is described here and decided nowhere. `server/nodedid/store.go`
holds one ed25519 keypair per node under `id = 'self'`, and no ADR specifies it.
ADR-012 made that gap load-bearing by accepting the browser as a node.
Tracked in #840.

A browser takes its signer identity from `laye-p2p` (`teranos/laye`), which
ships with QNTX web. laye-p2p mints an ed25519 keypair and persists it to
IndexedDB, so a tab holds the same key across reloads. That key is what a node
DID names — `did:key:z` + base58btc(`0xed 0x01` ‖ pubkey) — so a browser's
node DID is an encoding of its peer key, not a second identity.

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

### Implementation: Rust crate `ats-id`

Both layers are implemented in the Rust crate `ats-id`, maintained in this repository. The Go dependency on `teranos/vanity-id` has been retired (#793).

**Design principles:**

- **Review, don't transcribe.** Each function is reconsidered during migration. The acronym tables and heuristic bloat in vanity-id are not carried forward blindly.
- **Pure core, pluggable boundaries.** ID generation is pure computation. RNG and storage lookups stay at the caller.
- **No regex.** String methods only (CLAUDE.md).
- **Expose via WASM.** Both wazero (Go server) and browser (wasm-bindgen) targets, following the pattern established in ADR-005.

Shipped in phases, each independently releasable, and all of it landed except
one: **Vanity ID generation was dropped** — deriving a handle from a name is not
needed, so subjects carry names a human supplies. The generator also covers
non-attestation IDs (embedding IDs, run IDs), which is why it outgrew its name.

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
- ADR-012: Browser as First-Class Node — makes a browser a node, and so a signer
- `server/nodedid/` — existing Node DID infrastructure
