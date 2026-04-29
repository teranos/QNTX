# ADR-015: Search as a Plugin-Provided Service

## Status
Completed

## Context

QNTX has no search infrastructure. Plugins that need to find things — search documents by keyword, query attestations beyond exact field matching — have no service to call.

Fuzzy-ax exists as a workaround: approximate string matching over attestation queries. It's fragile, slow on large datasets, and conflates query parsing with search. The decision to deprecate fuzzy-ax has already been taken.

MeiliSearch is a proven full-text search engine with typo tolerance, faceted filtering, and sub-millisecond queries. Rather than building a custom search layer, QNTX should expose MeiliSearch through the same plugin-provided service pattern established by LLMService (ADR-014).

## Decision

Add SearchService as a plugin-provided gRPC service. A new plugin — `qntx-meili` (Rust) — owns the MeiliSearch instance and registers as the search backend. Core routes search calls to the provider. Consumers call `SearchService.Search` without knowing or caring about the underlying engine.

`qntx-meili` is a dedicated search provider plugin, separate from any domain plugin. Domain plugins are consumers of SearchService, not providers. `qntx-meili` lives in `qntx-plugins/qntx-meili/` in this repository.

This is the second plugin-provided service on `ServiceRegistry`, after LLMService.

## Protocol

See `plugin/grpc/protocol/search.proto`. Four RPCs: `Search`, `IndexDocuments`, `DeleteDocuments`, `ConfigureIndex`. Documents and filters flow as JSON bytes — the proto is engine-agnostic.

## Responsibility boundary

Domain plugins decide *what* gets indexed and *when* — they push JSON documents and search by query. `qntx-meili` decides *how* — index creation, schema detection, field configuration, async task management, and all MeiliSearch-specific mechanics. The proto is engine-agnostic: documents in, results out.

## Consumers

Any plugin that needs full-text search over indexed documents. A plugin calls `services.Search()` with a query and index name, gets ranked results back. The plugin does not need a MeiliSearch client or any knowledge of the search engine.

QNTX core is also a future consumer — AX queries can route through SearchService for full-text matching instead of fuzzy string approximation, providing the path to fuzzy-ax deprecation.

## Fuzzy-ax deprecation

Fuzzy-ax is string-level approximation over attestation fields. SearchService replaces it with actual search infrastructure — typo tolerance, relevance ranking, faceted filtering. The deprecation is incremental: new query paths use SearchService, existing fuzzy-ax paths are removed as consumers migrate. Not a priority now, but part of the plan.

## Routing

Same pattern as LLMService: `SearchServer` in core holds a reference to the provider backend. Provider plugins register via `SetService`. Callers go through `services.Search()` on `ServiceRegistry`.

## Startup ordering

If a plugin calls `services.Search()` before `qntx-meili` has initialized, the call fails. Consumer plugins should defer configuration (e.g. `ConfigureIndex`) and retry lazily rather than failing initialization — the search provider may register seconds after the consumer starts.

## Meili Panel Glyph

`qntx-meili` registers a panel glyph for index management visibility. Shows indexes, document counts, indexing status. Without this, the search infrastructure is a black box.

## Consequences

- `qntx-meili` is a standalone Rust plugin — search infrastructure lives in its own process
- Domain plugins stay decoupled from search internals — no MeiliSearch client dependencies
- Fuzzy-ax has a concrete replacement path
- Index management is `qntx-meili`'s concern — domain plugins push documents, the provider handles the rest
