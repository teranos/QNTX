# qntx-meili

Search provider plugin for QNTX. Routes SearchService gRPC RPCs to a MeiliSearch instance.

## Configuration

In `am.toml`:

```toml
[plugin]
enabled = ["meili"]

[meili]
url = "http://localhost:7700"
key = "your-master-key"
```

CLI defaults (`--meili-url`, `--meili-key`) are overridden by am.toml values.

## RPCs

- **Search** — full-text query against a named index, returns document JSON + hit count
- **IndexDocuments** — add/update documents (JSON bytes) to an index
- **DeleteDocuments** — remove documents by ID from an index

## Auth validation

On initialize, the plugin calls MeiliSearch's list-indexes endpoint to verify connectivity and API key. A bad key surfaces immediately in the logs rather than failing silently on the first search.

## Build

```
make install        # build release, install to ~/.qntx/plugins/, hot-restart if running
nix build           # reproducible build via Nix flake
```

## Limitations

**Strikethrough = verified.** A limitation is only struck through after end-to-end testing confirms the fix works.

- **SPR** — Single provider. Core routes all search RPCs to one plugin. No multi-provider fanout or fallback chain.

- **NRS** — No relevance score. MeiliSearch doesn't expose scores by default. `SearchHit.score` is always 0.0.

- **PKI** — Primary key is `"id"`. Both `index_documents` and hit ID extraction assume documents have an `"id"` field. Documents without it get an empty string ID.

- **NFI** — No filter support. `req.filters` JSON bytes are accepted in the proto but not applied to MeiliSearch queries.

- **GRO** — gRPC only. No HTTP proxy endpoint or frontend integration. Search is only accessible through the core's gRPC service mesh.

- **MPG** — MeiliSearch Panel Glyph (ADR-015). No UI for browsing indexes, running queries, or managing documents.
