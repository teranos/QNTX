# 10 Things — for a much better QNTX product

Ten concrete tasks that would change the user's experience of QNTX most.
Ordered by impact on what a user can do, see, and trust — not by ease of
implementation. The framing is the **product** (what users get), not
internal code cleanliness alone, though several items have both effects.

The pattern under the list: five items (#2, #3, #4, #6, #8) are about
**visibility** — surface what QNTX already does internally so users can
see and trust it. Two (#1, #5) are about **identity** — tell users what
the product is and show its distinctive value. Three (#7, #9, #10) are
**hygiene with user-facing payoff**. If forced to pick one, it's #2 —
in a provenance system, silent data loss destroys trust in everything else.

---

## 1. Tell users what QNTX *is* — a canonical landing + worked end-to-end path

A new user landing in the repo today confronts a substrate with no defining
use. The bio pipeline (Europe PMC → FASTA → amino-acids → Mol*/PDB →
AlphaFold → arc diagrams → GenBank) is already a real exemplar; turn it
into the canonical "this is how you use QNTX" experience — a single
landing doc that takes a user from zero to a working bio-knowledge use
case. Without this, every other improvement reaches fewer people.

## 2. Make eviction & distillation *lineage* visible

QNTX's thesis is provenance; the bounded-storage subsystem destroys
attestations on purpose. Two recent bugs were silent eviction — attestations
gone with no UI signal. Build a surface that answers: this disappeared,
why, when, into which sigma. `storage_events` (migration 010) and sigmas'
`_first_seen`/`_last_seen`/`_count` already hold the data; it isn't
exposed. In a provenance system this is the trust-critical feature.

## 3. Trace IDs across the gRPC boundary

A request through fetch → attestation → distill → embed → cluster
crosses ~5 processes and is currently unfollowable. The logger already
has `WithTraceID` / `FieldTraceID`; the missing piece is a client+server
gRPC interceptor pair that copies the trace ID into metadata. This
unlocks every other observability move (#4, #6, #8) because without
correlation, all of those are per-process.

## 4. A persistent failure / health surface

Errors today are transient toasts; users can't audit. Replace with a feed:
"things that have failed recently across server + plugins," plus plugin
health and rate-limiter state. Users need to answer "is QNTX OK right now?"
without reading logs.

## 5. Make the type system visible as the contested thing it is

This is QNTX's distinctive value proposition — types are attestations,
multi-actor, evolvable, can contradict. In the UI today it's a hidden
mechanism. The triplet glyph (`69ee5c48`) showed the model can be reified
visually; apply the same move to types: a navigable graph of types with
their actors, disagreements, and evolution. Changes QNTX in the user's
mind from "another database" to "the contested-knowledge thing."

## 6. Lightweight metrics with retention

Query latency p99, attestation creation rate, FetchService outbound rate
(the window limiter WARNs at 80% — but is the live rate visible? only in
logs), plugin RPC latency. Even a 7-day rolling window. Generalizes the
bespoke slow-log sparkline into something users can tune and trust.

## 7. Finish the embedding extraction per ADR-017

Embedding code currently lives in `server/embeddings/` (~2,800 lines) and
also as a plugin-provided-service interface — concern-split. Consolidate
into the subpackage, then extract to a plugin like the graph package was
(`e83f2cdf`). Effect for users: embedding becomes genuinely swappable per
the architecture's promise; "QNTX core without embedding" becomes a real
product configuration.

## 8. Watcher / meld activity surface

The reactive layer (per AXIOMS.md: "the edge IS a watcher"; meld DAGs
compile to watcher subscriptions) is the product's automation engine —
and it's invisible. Build a view showing what's subscribed, fire counts,
stalls. Same shape as #4 but for the reactive system specifically.
Currently a black box; should be the centerpiece of a knowledge-automation
product.

## 9. Server handler V1 scaffold + tests next to each handler

The HTTP handler layer in `server/` carries the system's worst complexity
hotspots and lowest test ratio. Extract a shared request/response scaffold
(`decodeJSON` / `writeJSON` / `writeError` / a field-validator) and convert
each handler to use it. The frontend already has its half (`assertOk`,
`jsonBody`, `apiJson<T>` — from `65f40f79`, `e18f9836`); the server doesn't.
Sounds internal but shows up as user-visible reliability — handlers are
the request boundary, where bugs hit users.

## 10. Settle the core noun: `As` → `Attestation` (TODO #605) + reconcile filter types

The most load-bearing noun in the system has two names (`As` in code,
"Attestation" in docs). And there are three `*Filter` shapes
(`AttestationFilter` Go, `AxFilter` Go, `AxFilter` Rust) that disagree at
the FFI boundary. Cosmetic for the code; product-changing for the
user-visible surface — error messages stop saying `As`, the SDK stops
being inconsistent, the docs stop drifting.
