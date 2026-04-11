# ADR-015: Search as a Plugin-Provided Service

## Status
Proposed

## Context

QNTX has no search infrastructure. Plugins that need to find things — search documents by keyword, query attestations beyond exact field matching — have no service to call.

Fuzzy-ax exists as a workaround: approximate string matching over attestation queries. It's fragile, slow on large datasets, and conflates query parsing with search. The decision to deprecate fuzzy-ax has already been taken.

MeiliSearch is a proven full-text search engine with typo tolerance, faceted filtering, and sub-millisecond queries. Rather than building a custom search layer, QNTX should expose MeiliSearch through the same plugin-provided service pattern established by LLMService (ADR-014).

## Decision

Add SearchService as a plugin-provided gRPC service. A new plugin — `qntx-meili` (Rust) — owns the MeiliSearch instance and registers as the search backend. Core routes search calls to the provider. Consumers call `SearchService.Search` without knowing or caring about the underlying engine.

`qntx-meili` is a dedicated search provider plugin, separate from any domain plugin. Domain plugins are consumers of SearchService, not providers.

This is the second plugin-provided service on `ServiceRegistry`, after LLMService.

## Protocol

`plugin/grpc/protocol/search.proto`

```protobuf
service SearchService {
  rpc Search(SearchRequest) returns (SearchResponse);
}

message SearchRequest {
  string query = 1;           // search query text
  string index = 2;           // which index to search
  int32 top_k = 3;            // max results to return
  map<string, string> filters = 4;  // facet filters
}

message SearchResponse {
  repeated SearchHit hits = 1;
  int32 total = 2;            // total matches (before top_k limit)
  int32 processing_ms = 3;
}

message SearchHit {
  string id = 1;
  float score = 2;
  map<string, string> fields = 3;   // document fields
}
```

## Consumers

Any plugin that needs full-text search over indexed documents. A plugin calls `services.Search()` with a query and index name, gets ranked results back. The plugin does not need a MeiliSearch client or any knowledge of the search engine.

QNTX core is also a future consumer — AX queries can route through SearchService for full-text matching instead of fuzzy string approximation, providing the path to fuzzy-ax deprecation.

## Fuzzy-ax deprecation

Fuzzy-ax is string-level approximation over attestation fields. SearchService replaces it with actual search infrastructure — typo tolerance, relevance ranking, faceted filtering. The deprecation is incremental: new query paths use SearchService, existing fuzzy-ax paths are removed as consumers migrate. Not a priority now, but part of the plan.

## Routing

Same pattern as LLMService: `SearchServer` in core holds a reference to the provider backend. Provider plugins register via `SetService`. Callers go through `services.Search()` on `ServiceRegistry`.

No queuing needed initially — MeiliSearch handles concurrent reads natively. If write contention becomes an issue (index updates during search), the provider handles it internally.

## Consequences

- `qntx-meili` is a standalone Rust plugin — search infrastructure lives in its own process
- Domain plugins stay decoupled from search internals — no MeiliSearch client dependencies
- Fuzzy-ax has a concrete replacement path
- Index management (creation, schema, updates) is `qntx-meili`'s responsibility, not core's
