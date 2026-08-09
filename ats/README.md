# ⋈ + = ⌬  ✦ ⟶

# ATS - Attestation Type System

ATS.

A Type System built on the Attestation primitive, as [subject] is [predicate] of [context] by [actor]

## ATS and QNTX

QNTX is heavily built on ATS — the server, plugins, glyphs, ꩜ Pulse and the CLI all reach
claims through it.

## Multiple database backends

Storage sits behind the interfaces in
[`store.go`](store.go) and the matching `ats` storage traits, and the backend is
selected by `[storage] backend` in `am.toml`:

| Backend | Where | Selected by |
|---|---|---|
| SQLite | server, CLI — via Rust (`crates/ats-sqlite`) | `backend = "sqlite"` (default) |
| Parquet | server — via DuckDB (`crates/ats-duckdb`) | `backend = "parquet"` |
| IndexedDB | browser tab (`crates/ats-indexeddb`) | the browser build |

See [ADR-023](../docs/adr/ADR-023-storage-backend-selection.md) (backend selection) and
[ADR-024](../docs/adr/ADR-024-parquet-storage-backend.md) (Parquet via DuckDB).

## ATS can run in the browser tab

| Concern | In the tab | Where |
|---|---|---|
| Model | ✓ | `crates/ats/src/attestation/` |
| Language (parse, classify, expand, temporal) | ✓ | `crates/ats/src/parser/`, `classify/`, `expand.rs`, `temporal.rs` |
| Store | ✓ | `crates/ats-indexeddb` — IndexedDB against the same `ats` storage traits |
| Reaction (watchers) | ✓ | `crates/ats/src/watcher.rs` |
| ⟶ `so` actions | ✗ | dispatch to ꩜ Pulse — server-side |
| gRPC `ATSStoreService` | ✗ | the remote surface, for plugins |

`crates/ats-indexeddb` matches the `ats` storage trait contract — same method names,
same inputs, same outputs, same error semantics.

## Why Attestations?

An attestation is a verifiable claim.

For example:

```
ENTITY-A   is member of   ORG-1                by hr-system@company       on 2025-01-15
PERSON-456 speaks         Dutch                by profile-system@platform since 2020-06-01
PATIENT-123 has diagnosis of "Type 2 Diabetes" by dr.smith@hospital       on 2025-01-10
```

Attestations (`As`) are structured claims with:

- **Subjects** — entities being described (can be multiple, for compound statements)
- **Predicates** — relationships or attributes
- **Contexts** — values or related entities
- **Actors** — entities making the claim
- **Temporal** — when the claim was made
- **Attributes** — additional metadata

**Subjects are claim-bearing names.** A subject names the entity being attested — `alice`, `vacancies`, `pulse`, `model:qwen-2.5-7b`. Never use UUIDs, database IDs, or numeric identifiers as subjects. The storage layer warns at write time when a subject looks id-like; see [docs/subjects.md](../docs/subjects.md).

The claim might be wrong. The actor might be unreliable. But the attestation itself is verifiable - someone did say this at this time.

Types themselves are attestations too - we attest that "restaurant" is a type with certain properties and searchable fields. This makes the type system itself transparent and evolvable. See [docs/attested-types.md](../docs/attested-types.md) for how type attestations work.

## Extensibility

ATS stays domain-agnostic through interfaces: `ActorDetector` (actor identification), `EntityResolver` (entity aliases), and `AttestationStore` and friends in [`store.go`](store.go) (any storage backend). Your domain logic plugs in without modifying core.

## Why ASIDs?

**Debugging/readability**: Seeing `AS-SARAH-AUTHOR-GITHUB-7K4M` in logs beats UUID gibberish. See [ADR-010](../docs/adr/ADR-010-identity-system.md).

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
- **`ax/` ⋈** - Query and retrieval ([see ax/README.md](ax/README.md))

**Store** — persistence and retrieval

- **`storage/`** - Backend implementations behind the interfaces in `store.go`
- **`wasm/`** - Bridge to the Rust engine in `crates/ats`

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
