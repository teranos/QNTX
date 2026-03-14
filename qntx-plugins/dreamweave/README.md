# DreamWeave

Weave timeline explorer — browse conversation history across projects and sessions.

OCaml Dream backend (gRPC plugin) + Svelte 5 frontend served via Bun.

## Architecture

```
QNTX Server
  ├── gRPC plugin protocol → dreamweave (OCaml/Dream, HTTP/2)
  │                            ├── /api/weaves — all weaves grouped by branch
  │                            └── /api/weaves/branch?name= — single branch
  └── /api/embeddings/clusters/memberships — cluster data

Frontend (Svelte 5, Bun.build)
  ├── dev-server.ts — Bun.serve(), proxies /api/ (h2c) and /qntx/ (HTTP/1.1)
  ├── build.ts — Bun.build() with Svelte compiler plugin
  └── src/App.svelte — single-file app
```

DreamWeave is a read-only query layer. The Dream backend fetches attestations from ATS via gRPC and serves them as JSON. The frontend fetches from both dreamweave (`/api/`) and QNTX server (`/qntx/`) for cluster data.

## Frontend

Svelte 5 single-file app. No bundler dependencies beyond Bun and svelte.

### What it does

- Vertical chronology, horizontal projects (grouped by branch prefix before `:`)
- TimeWarp: zoomable scrollbar with branch/session/cluster lanes, tool call diamonds, session/compaction seams, 12h time gaps
- Pointer-driven time-synchronized scrolling across columns
- Minimal markdown rendering for assistant turns (code blocks, bold, inline code)
- Click-to-copy weave attestation ID
- Turn selection with CMD+C copy
- Cluster membership visualization via QNTX embeddings API

### Frontend limitations

- **No virtualization**: every weave and turn is in the DOM. Will not scale to thousands of weaves.
- **No memoization**: `computeWarpItems()` and `parseTurns()` recompute on every render.
- **No live updates**: data fetched once on load. New weaves require manual refresh.
- **Cluster data is stale**: fetched once, never refreshed.
- **No search**: cannot find weaves by content, branch, or time range.
- **No URL routing**: no deep links to specific weaves or scroll positions. State lost on refresh.
- **Warp click math is fragile**: translateY/content fraction mapping breaks if CSS layout changes.
- **Time sync is coarse**: timestamp-nearest matching causes jumpy behavior with uneven weave density.
- **Raw text parsing**: `[speaker]` prefix parsing with string methods is fragile if text contains those patterns literally. A structured format from the API would be better.
- **Single file**: everything in App.svelte. Should split into components (WeaveCard, Turn, Warp, SessionHeader).

### Missing features (frontend)

- Diffs: show code changes that happened during the conversation.
- Git moments: commits and merges as distinct timeline events.
- Favorite weaves: bookmark and return to specific weaves.
- Freeze columns: toggle time-sync per column, pin a view in place.
- Hook/system messages: graunde hook messages are part of the conversation but not captured by loom yet.
- Images: screenshots as part of weave data, rendered inline.
- Branch click navigation: clicking a branch name should scroll to its weaves.
- Cluster legend: surface cluster labels, make cluster intelligence actionable.
- Adopt more QNTX/web styling patterns.

## Dream backend (OCaml)

gRPC plugin implementing `DomainPluginService`. Queries ATS for attestations with predicate `["Weave"]`, serves them as JSON over HTTP/2.

### Backend limitations

- **No caching**: queries ATS on every HTTP request. No in-memory cache.
- **No pagination**: no cursor or offset support. All weaves returned at once.
- **No HTTP filtering**: cannot filter by context, timestamp, actor, or text content via HTTP API.
- **No authentication**: HTTP endpoints are open (CORS: `*`). No per-request token validation.
- **No job handlers**: `ExecuteJob` is stubbed out.
- **No configuration schema**: `ConfigSchema` returns empty.
- **Silent error handling**: ATS connection errors logged to stderr only, not propagated. Initialize decode errors silently ignored.
- **No connection timeout**: synchronous socket connect to ATS with no timeout.
- **Hardcoded predicate**: can only query `["Weave"]` attestations.
- **Data loss on extraction**: `int_of_float` truncates number values. Missing subjects/contexts default to empty string with no error signal.
- **Fragile gRPC routing**: content-type check uses substring match on first 16 chars.
- **Port retry loop**: tries up to 10 ports if assigned port is taken; no fast failure.

## Upstream gaps (loom, graunde)

DreamWeave as consumer reveals what the upstream producers don't capture yet:

- **Hooks/system messages**: graunde and other hook messages are part of the conversation but loom doesn't include them in the weave.
- **Diffs**: code changes during a session are not recorded.
- **Git events**: commits, merges, branch operations are not weave events.
- **Images/screenshots**: not part of the weave data model.
- **Directory identity**: graunde's project tracking (which directory a session belongs to) has had drift issues, now resolved but still fragile.
- **Structured turns**: weave text is a flat string with `[speaker]` prefixes. A structured format (array of typed turns) would eliminate parsing fragility.

## Development

```sh
cd frontend
bun run dev-server.ts    # builds + serves on :5177, proxies to dreamweave + QNTX
bun run build.ts         # one-shot build to dist/
```

Requires dreamweave plugin running (`make dreamweave-plugin && make dev`) and QNTX server on port 8773.
