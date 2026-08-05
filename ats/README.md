# ⋈ + = ⌬  ✦ ⟶

# ATS - Attestation Type System

> **This file is the authoritative definition of ATS.** Every other mention in this
> repository links here rather than restating it. If they disagree, this file wins.

**ATS is the language of attestations.** Think `.ats` — a way to write a claim down, name
it, store it, and read it back.

*(Not an Applicant Tracking System.)*

ATS is all three of these at once:

- **A type system** — the data model and structure of attestations. Types are themselves attestations.
- **A store** — persistence and retrieval, behind interfaces that admit any backend.
- **A query language** — `⋈ ax`, which is *part of* ATS, not a sibling of it.

Together, these provide a domain-agnostic framework for attesting and ax-ing about entities.

## ATS and QNTX

QNTX is heavily built on ATS — the server, plugins, glyphs, ꩜ Pulse and the CLI all reach
claims through it.

ATS is also a **spinoff target**. It is meant to leave this repository, the way
[`teranos/errors`](https://github.com/teranos/errors) and
[pyre](https://github.com/teranos/pyre) already have, and the way `glyph/` is being
prepared to. Treat `ats/` as a library that currently happens to live here.

## Multiple database backends

ATS is not bound to one database. Storage sits behind the interfaces in
[`store.go`](store.go) and the matching `qntx-core` storage traits, and the backend is
selected by `[storage] backend` in `am.toml`:

| Backend | Where | Selected by |
|---|---|---|
| SQLite | server, CLI — via Rust (`crates/qntx-sqlite`) | `backend = "sqlite"` (default) |
| Parquet | server — via DuckDB (`crates/qntx-duckdb`) | `backend = "parquet"` |
| IndexedDB | browser tab (`crates/qntx-indexeddb`) | the browser build |

See [ADR-023](../docs/adr/ADR-023-storage-backend-selection.md) (backend selection) and
[ADR-024](../docs/adr/ADR-024-parquet-storage-backend.md) (Parquet via DuckDB).

## Most of ATS runs in your browser tab

ATS is not a server feature you call over the network. The bulk of it is Rust compiled to
WASM (`crates/qntx-core`), running the same code in the tab and on the server:

| Concern | In the tab | Where |
|---|---|---|
| Model | ✓ | `crates/qntx-core/src/attestation/` |
| Language (parse, classify, expand, temporal) | ✓ | `crates/qntx-core/src/parser/`, `classify/`, `expand.rs`, `temporal.rs` |
| Store | ✓ | `crates/qntx-indexeddb` — IndexedDB against the same `qntx-core` storage traits |
| Reaction (watchers) | ✓ | `crates/qntx-core/src/watcher.rs` |
| ⟶ `so` actions | ✗ | dispatch to ꩜ Pulse — server-side |
| gRPC `ATSStoreService` | ✗ | the remote surface, for plugins |

`crates/qntx-indexeddb` matches the `qntx-core` storage trait contract — same method names,
same inputs, same outputs, same error semantics — so the browser is a full ATS node, not a
thin client.

## Why ATS?

Traditional databases ask: "What is the schema?" They assume you know the structure upfront, bake it into code, and treat data as facts.

**The problem**: Real systems are about claims, not facts. You don't know if `hr-system@company` is right that Alice works here - you know that the HR system *said* it. Provenance matters. Attribution matters. Time matters.

Without attestations, you either:
- **Trust blindly** - store data as facts, lose who said what
- **Build attribution yourself** - reinvent metadata tracking in every table, inconsistently

**ATS is the answer**: Treat data as verifiable claims from the start. Every piece of information knows who attested to it and when.

## Why Attestations?

An attestation is a verifiable claim, not a fact.

At its simplest, an attestation is a statement of the form:

```
as [Subject] is [Predicate] of [Context] by [⌬ Actor] at [✦ Temporal]
```

This pattern captures:
- **What** was claimed (subject, predicate, context)
- **Who** claimed it (actor)
- **When** they claimed it (temporal)

**Subjects are claim-bearing names, not identifiers.** A subject names the entity being attested — `alice`, `vacancies`, `pulse`, `model:qwen-2.5-7b`. Never use UUIDs, database IDs, or numeric identifiers as subjects. The storage layer warns at write time when a subject looks id-like; see [docs/subjects.md](../docs/subjects.md).

The claim might be wrong. The actor might be unreliable. But the attestation itself is verifiable - someone did say this at this time.

Types themselves are attestations too - we attest that "restaurant" is a type with certain properties and searchable fields. This makes the type system itself transparent and evolvable. See [docs/attested-types.md](../docs/attested-types.md) for how type attestations work.

## Extensibility

ATS stays domain-agnostic through interfaces: `ActorDetector` (actor identification), `EntityResolver` (entity aliases). Your domain logic plugs in without modifying core.

## Why ASIDs?

**Debugging/readability**: Seeing `as-node_type-contact` in logs beats UUID gibberish.

**Vanity IDs for fundamentals**: Type definitions and other canonical attestations deserve stable, well-known IDs that systems can reference consistently. The alias system then maps duplicates to these canonical IDs.

## Features

- **ASID generation** with vanity ID support and collision detection
- **Attestation existence checking** to prevent duplicates
- **[REST API](../docs/api/attestations.md)** for querying and creating attestations over HTTP
- **[gRPC API](../docs/api/grpc-atsstore.md)** for plugin access to attestation storage (includes server-side streaming)

## Packages

**Model** — what a claim is

- **`types/`** - Attestation data model and type definitions

**Identity** — the names a claim carries

- **`identity/`** - ASID generation
- **`signing/`** - Self-certifying attestations
- **`alias/`** - Identity resolution and equivalence
- **`attrs/`** - Attribute schemas

**Language** — writing claims and reading them back

- **`parser/`** - Command parsing ([see parser/README.md](parser/README.md))
- **`ax/` ⋈** - Query and retrieval ([see ax/README.md](ax/README.md)) — part of ATS, not a sibling

**Store** — persistence and retrieval

- **`storage/`** - Backend implementations behind the interfaces in `store.go`
- **`wasm/`** - Bridge to the Rust engine in `crates/qntx-core`

**Acting on claims**

- **`watcher/`** - Fires on arriving claims
- **`so/` ⟶** - Semantic operations, dispatched to ꩜ Pulse ([see so/README.md](so/README.md))

## Testing

```bash
# Run ats package tests
go test ./ats/...

# Run with verbose output
go test ./ats/... -v

```
