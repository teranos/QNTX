# 10 Things — for a much better QNTX product

Ten tasks ordered by user impact — what changes for someone *using* QNTX,
not by ease of implementation. Each entry is the product framing only;
the technical detail and tracking live in ACTIONS / ITEMS (referenced
per item).

Pattern: five (#2, #3, #4, #6, #8) are **visibility** — surface what
QNTX already does internally so users can see and trust it. Two (#1, #5)
are **identity** — tell users what the product is and show its
distinctive value. Three (#7, #9, #10) are **hygiene with user-facing
payoff**. If forced to pick one, #2 — in a provenance system, silent
data loss destroys trust in everything else.

---

## 1. Tell users what QNTX *is* — a canonical landing + worked end-to-end path

A new user lands in the repo today with no defining use to anchor to.
The bio pipeline (Europe PMC → FASTA → amino-acids → Mol*/PDB →
AlphaFold → arc → GenBank) is already a real exemplar; promote it into
the canonical "how you use QNTX" path. Gating issue for adoption — every
other improvement reaches fewer people without it.
*Tracked in: ITEMS C.16.*

## 2. Make eviction & distillation lineage visible

QNTX's thesis is provenance; bounded storage destroys attestations on
purpose. Recent bugs were silent eviction — attestations gone with no
UI signal. A user has to be able to ask "this disappeared, why?" and
get an answer.
*Tracked in: ITEMS C.17.*

## 3. Trace IDs across the gRPC boundary

A request through fetch → attest → distill → embed → cluster crosses
~5 processes and is currently unfollowable. Foundation for every other
observability move; the logger already has the primitive.
*Tracked in: ACTIONS 9.*

## 4. A persistent failure / health surface

Errors today are transient toasts; users can't audit. Build a feed of
recent failures across server + plugins, plus plugin health and
rate-limiter state. So users can answer "is QNTX OK?" without reading
logs.
*Tracked in: ITEMS C.18.*

## 5. Make the type system visible as the contested thing it is

QNTX's distinctive value proposition — types are attestations,
multi-actor, evolvable, can contradict — lives in the UI as a hidden
mechanism. Surface it. Changes QNTX in the user's mind from "another
database" to "the contested-knowledge thing."
*Tracked in: ITEMS C.19.*

## 6. Lightweight metrics with retention

Query latency p99, attestation creation rate, FetchService outbound
rate, plugin RPC latency. Even a 7-day rolling window. Lets users tune
and trust.
*Tracked in: ACTIONS 10.*

## 7. Finish the embedding extraction per ADR-017

Embedding code lives in `server/embeddings/` *and* as a plugin-provided
service — concern-split. Finish the move so embedding becomes genuinely
swappable per the architecture's promise.
*Tracked in: ITEMS B.6.*

## 8. Watcher / meld activity surface

The reactive layer is the product's automation engine, currently
invisible. Show what's subscribed, what's firing, what's stalled.
Should be a centerpiece, not a black box.
*Tracked in: ITEMS C.20.*

## 9. Server handler V1 — shared request/response scaffold + tests

The handler layer carries the worst complexity and the lowest test
ratio. A shared scaffold collapses the cyclo cluster and dissolves the
duplication; each handler becomes small enough to test. Shows up as
user-visible reliability since handlers are the request boundary.
*Tracked in: ITEMS C.21.*

## 10. Settle the core noun: `As` → `Attestation` + reconcile filter types

The most load-bearing noun has two names. Three `*Filter` shapes
disagree at the FFI boundary. Cosmetic for the code; product-changing
for the user-visible surface — error messages, SDKs, and docs stop
drifting.
*Tracked in: ITEMS C.8 + Section F #4.*
