# qntx-loom

Receives conversation events from [Graunde](https://github.com/teranos/graunde) over UDP and stitches them into embedding-sized text blocks (weaves). Serves a timeline explorer frontend for browsing conversation history across projects and sessions.

**UDP port: 19470** — Graunde sends attestation JSON datagrams here. Fire-and-forget, no response.

## Architecture

```
QNTX Server
  ├── gRPC plugin protocol → loom (OCaml, HTTP/2)
  │                            ├── UDP listener (port 19470) — receives events from Graunde
  │                            ├── ATS reader — weaves OTLPSpan attestations from ix-otlp
  │                            ├── Stitcher — chunks turns into weaves, writes to ATS
  │                            ├── HTTP API (port 5178)
  │                            │     ├── GET /api/weaves — all weaves grouped by branch
  │                            │     ├── GET /api/weaves/branch?name= — single branch
  │                            │     ├── POST /api/import — JSONL import (in-process)
  │                            │     └── POST /api/import/otlp — weave OTLPSpan attestations
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

### What it does

- Vertical chronology, horizontal projects (grouped by branch prefix before `:`)
- All detected projects as columns: unweaved projects appear as empty columns ready for import
- Temporal alignment: discrete time spacers between weaves (1h base, exponential doubling, ~11 month range in 192px)
- TimeWarp: zoomable scrollbar with branch/session/cluster lanes, tool call diamonds, session/compaction seams
- Cluster distribution per project in drawer header
- Pointer-driven time-synchronized scrolling across columns
- Minimal markdown rendering for assistant turns (code blocks, bold, inline code)
- Click-to-copy weave attestation ID
- Turn selection with CMD+C copy
- Session browser with JSONL import from project headers

### Frontend limitations

- **No virtualization**: every weave and turn is in the DOM. Will not scale to thousands of weaves.
- **No memoization**: `computeWarpItems()` and `parseTurns()` recompute on every render.
- **No live updates**: data fetched once on load. New weaves require manual refresh.
- **Cluster data is stale**: fetched once, never refreshed.
- **No search**: cannot find weaves by content, branch, or time range.
- **No URL routing**: no deep links to specific weaves or scroll positions. State lost on refresh.
- **No client-side persistence**: column order, expanded/collapsed state, scroll positions, favorites, and UI preferences are lost on refresh. Column positions jump after import because the sort key changes when a project transitions from empty to woven. IndexedDB is the key next step — it unblocks favorites, stable column order, collapsible columns, frozen columns, and scroll position recall.
- **Warp click math is fragile**: translateY/content fraction mapping breaks if CSS layout changes.
- **Raw text parsing**: `[speaker]` prefix parsing with string methods is fragile if text contains those patterns literally. A structured format from the API would be better.

### Missing features (frontend)

- **Interactive cluster chips**: cluster distribution shows in the drawer but clicking a chip does nothing yet. Should filter/highlight weaves belonging to that cluster, scroll to them, or cross-highlight across columns.
- **Collapsible project columns**: minimize a project to a thin vertical strip showing just the project name (rotated). Click to restore. Keeps the column present but out of the way.
- **Favorite weaves**: bookmark and return to specific weaves. Requires IndexedDB for persistence.
- Diffs: show code changes that happened during the conversation.
- Git moments: commits and merges as distinct timeline events.
- Freeze columns: toggle time-sync per column, pin a view in place.
- Images: screenshots as part of weave data, rendered inline.
- Branch click navigation: clicking a branch name should scroll to its weaves.
- Cluster legend: surface cluster labels, make cluster intelligence actionable.
- Share components with QNTX/web: import actual UI components rather than reimplementing.

## HTTP API (OCaml)

Serves JSON over HTTP/2 on port 5178. Read endpoints query ATS for `["Weave"]` attestations. The import endpoint reads JSONL files in-process, feeds the stitcher pipeline, and writes weaves to ATS.

### HTTP API limitations

- **No caching**: queries ATS on every HTTP request. No in-memory cache.
- **No pagination**: no cursor or offset support. All weaves returned at once.
- **No HTTP filtering**: cannot filter by context, timestamp, actor, or text content via HTTP API.
- **No authentication**: HTTP endpoints are open (CORS: `*`). No per-request token validation.
- **No configuration schema**: `ConfigSchema` returns empty.
- **Silent error handling**: ATS connection errors logged to stderr only, not propagated. Initialize decode errors silently ignored.
- **No connection timeout**: synchronous socket connect to ATS with no timeout.
- **Hardcoded predicate**: can only query `["Weave"]` attestations.
- **Data loss on extraction**: `int_of_float` truncates number values. Missing subjects/contexts default to empty string with no error signal.

## Upstream gaps (graunde)

Loom as consumer reveals what the upstream producers don't capture yet:

- **Diffs**: code changes during a session are not recorded.
- **Git events**: commits, merges, branch operations are not weave events.
- **Images/screenshots**: not part of the weave data model.
- **Directory identity**: graunde's project tracking (which directory a session belongs to) has had drift issues, now resolved but still fragile.
- **Structured turns**: weave text is a flat string with `[speaker]` prefixes. A structured format (array of typed turns) would eliminate parsing fragility.

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

## OTLP Ingestion (Agno / OpenTelemetry)

Third ingestion path. The `ix-otlp` plugin receives OTLP/HTTP JSON trace exports and persists each span as an `OTLPSpan` attestation in ATS. Loom reads these attestations and weaves them — either automatically on startup (catch-up) or on demand via `POST /api/import/otlp`.

```
Agno Agent → OTLP exporter → ix-otlp plugin → OTLPSpan attestations (ATS)
                                                        ↓
                              loom (ats_reader.ml) → stitcher → Weaves
```

Traces persist as attestations regardless of whether loom is running. Loom catches up on startup.

### Span → turn mapping

| Span attribute / name | Label | Source |
|---|---|---|
| `gen_ai.operation.name = "invoke_agent"` | `[session]` | `gen_ai.agent.name` |
| `gen_ai.operation.name = "chat"` (events) | `[human]` + `[assistant]` | prompt/completion events |
| `tool.*` | `[tool]` / `[read]` / `[edit]` / `[search]` | tool name + input |
| Other spans with `gen_ai.agent.name` | `[agent]` | agent name + span name |

### Branch/context derivation

- **Branch**: `{qntx.project || service.name || "agno"}:{agent_name}` — derived by ix-otlp from OTLP resource attributes
- **Context**: `trace:{trace_id}` — each OTLP trace maps to one session

### Agno configuration

Point the OTLP HTTP exporter at ix-otlp via QNTX:

```sh
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:877/api/ix-otlp
```

Weaves created from OTLP traces have `weave_source: "agno-otel"`.

## Development

```sh
cd frontend
bun run dev-server.ts    # builds + serves on :5177, proxies to loom + QNTX
bun run build.ts         # one-shot build to dist/
```

Requires loom plugin running (`make loom-plugin && make dev`) and QNTX server on port 8773.
