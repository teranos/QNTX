# ADR-021: Graph Database as a Plugin-Provided Service

## Status
Proposed

## Context

QNTX removed its built-in graph visualization layer (D3 force-directed). The attestation substrate remains inherently relational — subjects link to predicates, types define relationships, watchers trace edges — but traversal queries are served by SQLite joins today.

Graph databases excel at multi-hop traversals, path finding, and pattern matching that become expensive in relational stores as depth increases. The same plugin-provided service pattern used for search (ADR-015, MeiliSearch) and LLM (ADR-014) applies here: core doesn't own the engine, a plugin does.

## Decision

If a graph database becomes necessary, it will be exposed as a gRPC plugin-provided service — `GraphService` on the `ServiceRegistry`. A plugin owns the graph engine and registers as provider. Core and other plugins call `services.Graph()` without engine coupling.

This is not committed work. It's a placeholder for when traversal queries outgrow SQLite's capabilities.

Neo4j is ruled out: JVM dependency, heavy memory footprint, and it's the obvious choice — not interesting when newer embeddable engines exist.

## Candidates to research

**CozoDB** — Rust embeddable, MPL-2.0. Datalog-based query language gives recursive multi-hop traversal and cycle detection natively. Built-in graph algorithms (shortest path, PageRank). ~50MB footprint, 100K QPS. Embeds as a crate — no subprocess. Risk: pre-1.0, single maintainer.

**SurrealDB** — Rust embeddable, BSL 1.1. Multi-model (document + graph). 3.0 GA, corporate backing. Graph traversal via RELATE/arrow syntax. Weaker graph semantics than Datalog (no recursive traversal primitive). Embeds via `surrealdb` crate.

**Memgraph** — C++ single binary, BSL 1.1. Full openCypher, in-memory, <1s startup. Subprocess model (same as qntx-meili). Speaks Bolt protocol — `neo4rs` Rust crate works unchanged. Linux-only (macOS blocked until Linux deploy target exists).

CozoDB and SurrealDB embed directly into a Rust plugin binary — same deployment model as linking RocksDB. Memgraph runs as a subprocess like MeiliSearch.

## When this triggers

- Path queries deeper than 3 hops become a recurring pattern
- Watcher fan-out needs subgraph-scoped matching
- Attestation lineage/provenance tracking requires cycle detection

## Shape

Same as ADR-015: engine-agnostic proto, plugin owns all engine mechanics, consumers submit queries and get results. Likely RPCs: `Traverse`, `ShortestPath`, `Subgraph`, `Sync` (bulk attestation→node projection).

## Not in scope

Graph visualization. The UI renders glyphs on a canvas — layout is a frontend concern, not a database concern.
