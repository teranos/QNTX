# qntx-loom

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

- **All detected projects as columns**: show every Claude project from session discovery, not just ones with existing weaves. Unweaved projects appear as empty columns ready for import.
- **Cluster distribution per project**: in the expanded project header, show how the project's weaves distribute across available clusters (% membership per cluster).
- **Temporal alignment across columns**: columns should visually offset based on real time distance. A March 4 weave in one column should not sit at the same scroll height as a March 11 weave in another — the gap should reflect the actual time delta, shrinking dynamically as timestamps converge.
- **Collapsible project columns**: minimize a project to a thin vertical strip showing just the project name (rotated). Click to restore. Keeps the column present but out of the way.
- Diffs: show code changes that happened during the conversation.
- Git moments: commits and merges as distinct timeline events.
- Favorite weaves: bookmark and return to specific weaves.
- Freeze columns: toggle time-sync per column, pin a view in place.
- ~~Hook/system messages~~: done — graunde hook messages flow as `[hook]` turns in weaves, rendered with red accent.
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

- ~~**Hooks/system messages**~~: done — loom captures `[hook]` turns via UDP from graunde, renders them with red accent and warp dot.
- **Diffs**: code changes during a session are not recorded.
- **Git events**: commits, merges, branch operations are not weave events.
- **Images/screenshots**: not part of the weave data model.
- **Directory identity**: graunde's project tracking (which directory a session belongs to) has had drift issues, now resolved but still fragile.
- **Structured turns**: weave text is a flat string with `[speaker]` prefixes. A structured format (array of typed turns) would eliminate parsing fragility.

## JSONL Import (historical weaving)

Loom currently only weaves live sessions via UDP from Graunde. The JSONL import path enables weaving historical conversations — sessions that predate Graunde/Loom, or completing sessions where UDP delivery was partial.

One JSONL file = one session. Claude Code writes all turns to `~/.claude/projects/{project-slug}/{session-uuid}.jsonl`. The format is Claude Code native (not attestation format): `type: "user"` / `"assistant"` / `"progress"`, with `message`, `gitBranch`, `sessionId`, `cwd`, `timestamp`.

### Session states

Each session has one of four states, derivable from ATS + filesystem:

1. **Unweaved** — JSONL on disk, no weaves in ATS for this session
2. **Partial** — weaves exist (from Graunde UDP), no `WeaveComplete` attestation, potentially incomplete
3. **Complete** — `WeaveComplete` attestation exists, JSONL was fully imported at that point
4. **Stale** — `WeaveComplete` exists but JSONL has grown since import (file size > recorded size), new unweaved content at the tail

Completeness tracked via `WeaveComplete` attestation written by loom after import, recording file size/line count. Re-import processes only lines past the previous import point.

### Import flow

1. Frontend "N sessions" is clickable → opens session browser
2. Session browser lists all projects and their JSONL sessions with state indicators
3. User selects an unweaved or stale session to import
4. Frontend POSTs file path to loom's HTTP API (`POST /api/import`)
5. Loom reads the JSONL in-process, extracts turns, chunks via existing pipeline, writes weaves to ATS
6. Loom writes `WeaveComplete` attestation on success
7. Frontend refreshes, shows the new weaves

## Development

```sh
cd frontend
bun run dev-server.ts    # builds + serves on :5177, proxies to loom + QNTX
bun run build.ts         # one-shot build to dist/
```

Requires loom plugin running (`make loom-plugin && make dev`) and QNTX server on port 8773.
