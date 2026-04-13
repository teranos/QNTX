# qntx-loom

TODO: This README needs restructuring. Not everything is relevant, I think a lot of implementation details slipped in. Splitting off LIMITATIONS.md is a good idea for example

Receives conversation events from [Graunde](https://github.com/teranos/graunde) over UDP and stitches them into embedding-sized text blocks (weaves). Serves a timeline explorer frontend for browsing conversation history across projects and sessions.

**UDP port: 19470** — Graunde sends attestation JSON datagrams here. Fire-and-forget, no response.

## Architecture

```
QNTX Server
  ├── gRPC plugin protocol → loom (OCaml, HTTP/2)
  │                            ├── UDP listener (port 19470) — receives events from Graunde
  │                            ├── Stitcher — chunks turns into weaves, writes to ATS
  │                            ├── HTTP API (port 5178)
  │                            │     ├── GET /api/weaves — all weaves grouped by branch
  │                            │     ├── GET /api/weaves/branch?name= — single branch
  │                            │     └── POST /api/import — JSONL import (in-process)
  │                            └── Serialize UI — attestation-to-JSON for the frontend
  └── /api/embeddings/clusters/memberships — cluster data

Frontend (Svelte 5, Bun.build)
  ├── dev-server.ts — Bun.serve(), proxies /api/ (h2c) and /qntx/ (HTTP/1.1)
  ├── build.ts — Bun.build() with Svelte compiler plugin
  └── src/App.svelte — single-file app
```

The HTTP API is a read-only query layer. It fetches attestations from ATS via gRPC and serves them as JSON. The frontend fetches from both loom (`/api/`) and QNTX server (`/qntx/`) for cluster data.

## UDP event format

Every datagram is a JSON attestation:

```json
{
  "subjects": ["tmp3/QNTX:feat/branch-name"],
  "predicates": ["UserPromptSubmit"],
  "contexts": ["session:abc-123"],
  "attributes": { "prompt": "..." }
}
```

Graunde sends these on every hook event via `attestEvent` → `sendToLoom`. Corrective stop hooks additionally send `Hook` predicates via `notifyLoomHook`.

## Turn types

| Predicate | Label | Source |
|---|---|---|
| `UserPromptSubmit` | `[human]` | `attributes.prompt` |
| `Stop` | `[assistant]` | `attributes.last_assistant_message` |
| `PreToolUse` (Bash) | `[tool]` | `attributes.tool_input.command` (whitelist-filtered) |
| `PreToolUse` (Edit) | `[edit]` | `attributes.tool_input.file_path` |
| `PreToolUse` (Read) | `[read]` | `attributes.tool_input.file_path` |
| `PreToolUse` (Grep/Glob) | `[search]` | `attributes.tool_input.pattern` |
| `PreToolUse` (Write) | `[write]` | `attributes.tool_input.file_path` |
| `Hook` | `[hook]` | `attributes.hook_output` |
| `SessionStart` | `[session]` | `attributes.session_id` |
| `SessionEnd` | `[session]` | `attributes.session_id` |
| `PreCompact` | `[compaction]` | static marker |
| `SubagentStart/Stop` | `[agent]` | `attributes.agent_type` |
| `TaskCompleted` | `[task]` | `attributes.task_subject` |

Bash `[tool]` turns are filtered by a command whitelist (git, gh, make). All other tool types (Edit, Read, Grep, Glob, Write) are captured with their own labels.

## Frontend

Svelte 5 app with extracted components. No bundler dependencies beyond Bun and svelte.

TODO:
- RESEARCH usage of Glyphs package in Loom: https://jsr.io/@qntx/glyphs/versions

### What it does

- Vertical chronology, horizontal projects (grouped by branch prefix before `:`)
- All detected projects as columns: unweaved projects appear as empty columns ready for import
- Temporal alignment: discrete time spacers between weaves (1h base, exponential doubling, ~11 month range in 192px)
- TimeWarp: zoomable scrollbar with branch/session/cluster lanes, tool call diamonds, session/compaction seams
- Cluster distribution per project in drawer header
- Pointer-driven time-synchronized scrolling across columns
- Minimal markdown rendering for assistant turns (code blocks, bold, inline code)
- Token-aware rendering for scry weaves: per-token confidence coloring (brown/amber tint scaling with uncertainty), hover tooltips showing confidence, entropy, top_gap, and top-k candidates with probabilities
- Click-to-copy weave attestation ID
- Turn selection with CMD+C copy
- Session browser with JSONL import from project headers

### Frontend limitations

- **NVIR** — No virtualization. Every weave and turn is in the DOM. Will not scale to thousands of weaves.
- **NMEM** — No memoization. `computeWarpItems()` and `parseTurns()` recompute on every render.
- **NLIV** — No live updates. Data fetched once on load. New weaves require manual refresh.
- **CLSS** — Cluster data is stale. Fetched once, never refreshed.
- **NSCH** — No search. Cannot find weaves by content, branch, or time range.
- **NURL** — No URL routing. No deep links to specific weaves or scroll positions. State lost on refresh.
- **NCSP** — No client-side persistence. Column order, expanded/collapsed state, scroll positions, favorites, and UI preferences are lost on refresh. Column positions jump after import because the sort key changes when a project transitions from empty to woven. IndexedDB is the key next step — it unblocks FAVE, stable column order, CPC, FRZC, and scroll position recall.
- **WCMF** — Warp click math is fragile. translateY/content fraction mapping breaks if CSS layout changes.
- **RTP** — Raw text parsing. `[speaker]` prefix parsing with string methods is fragile if text contains those patterns literally. A structured format from the API would be better (see UST).

### Missing features (frontend)

- **ICC** — Interactive cluster chips. Cluster distribution shows in the drawer but clicking a chip does nothing yet. Should filter/highlight weaves belonging to that cluster, scroll to them, or cross-highlight across columns.
- **CPC** — Collapsible project columns. Minimize a project to a thin vertical strip showing just the project name (rotated). Click to restore. Keeps the column present but out of the way.
- **FAVE** — Favorite weaves. Bookmark and return to specific weaves. Requires IndexedDB for persistence (NCSP).
- **FDIF** — Frontend diffs. Show code changes that happened during the conversation. Gated on UDIF.
- **GITM** — Git moments. Commits and merges as distinct timeline events. Gated on UGIT.
- **FRZC** — Freeze columns. Toggle time-sync per column, pin a view in place.
- **IMG** — Images inline. Screenshots as part of weave data, rendered inline. Gated on UIMG.
- **BCN** — Branch click navigation. Clicking a branch name should scroll to its weaves.
- **CLGD** — Cluster legend. Surface cluster labels, make cluster intelligence actionable.
- **SCMP** — Share components with QNTX/web. Import actual UI components rather than reimplementing.

## HTTP API (OCaml)

Serves JSON over HTTP/2 on port 5178. Read endpoints query ATS for `["Weave"]` attestations. The import endpoint reads JSONL files in-process, feeds the stitcher pipeline, and writes weaves to ATS.

### HTTP API limitations

- **NCAC** — No caching. Queries ATS on every HTTP request. No in-memory cache.
- **NPAG** — No pagination. No cursor or offset support. All weaves returned at once.
- **NFLT** — No HTTP filtering. Cannot filter by context, timestamp, actor, or text content via HTTP API.
- **NAUT** — No authentication. HTTP endpoints are open (CORS: `*`). No per-request token validation.
- **NCFG** — No configuration schema. `ConfigSchema` returns empty.
- **SEH** — Silent error handling. ATS connection errors logged to stderr only, not propagated. Initialize decode errors silently ignored.
- **NCTO** — No connection timeout. Synchronous socket connect to ATS with no timeout.
- **HCP** — Hardcoded predicate. Can only query `["Weave"]` attestations.
- **DLE** — Data loss on extraction. `int_of_float` truncates number values. Missing subjects/contexts default to empty string with no error signal.

## Upstream gaps (graunde)

Loom as consumer reveals what the upstream producers don't capture yet:

- **UDIF** — Diffs. Code changes during a session are not recorded. Blocks FDIF.
- **UGIT** — Git events. Commits, merges, branch operations are not weave events. Blocks GITM.
- **UIMG** — Images/screenshots. Not part of the weave data model. Blocks IMG.
- **UDID** — Directory identity. Graunde's project tracking (which directory a session belongs to) has had drift issues, now resolved but still fragile.
- **UST** — Structured turns. Weave text is a flat string with `[speaker]` prefixes. A structured format (array of typed turns) would eliminate RTP fragility.

## JSONL Import (historical weaving)

Weaves historical conversations — sessions that predate Graunde/Loom, or completing sessions where UDP delivery was partial. Imports the full JSONL file each time (not incremental).

One JSONL file = one session. Claude Code writes all turns to `~/.claude/projects/{project-slug}/{session-uuid}.jsonl`. The format is Claude Code native (not attestation format): `type: "user"` / `"assistant"` / `"progress"`, with `message`, `gitBranch`, `sessionId`, `cwd`, `timestamp`.

### Session states

Each session has one of four states, derivable from ATS + filesystem:

1. **Unweaved** — JSONL on disk, no weaves in ATS for this session
2. **Partial** — weaves exist (from Graunde UDP), no `WeaveComplete` attestation, potentially incomplete
3. **Complete** — `WeaveComplete` attestation exists, JSONL was fully imported at that point
4. **Stale** — `WeaveComplete` exists but JSONL has grown since import (file size > recorded size), new unweaved content at the tail

Completeness tracked via `WeaveComplete` attestation written by loom after import, recording file size/line count.

### Weave source precedence

Weaves carry a `weave_source` attribute: `"graunde"` (live UDP) or `"jsonl"` (JSONL import). When a `WeaveComplete` exists for a session, JSONL weaves take precedence — graunde weaves for that session are suppressed in the read path. No attestations are deleted.

### Import flow

1. Open the session browser from a project header
2. Click import on any unweaved, partial, or stale session
3. Loom reads the JSONL, chunks via the stitcher, writes weaves to ATS
4. `WeaveComplete` attestation records the import; weaves appear immediately

## Development

```sh
cd frontend
bun run dev-server.ts    # builds + serves on :5177, proxies to loom + QNTX
bun run build.ts         # one-shot build to dist/
```

Requires loom plugin running (`make loom-plugin && make dev`) and QNTX server on port 8773.
