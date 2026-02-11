# Glyph Attestation Flow — Development Plan

**Vision:** [docs/vision/glyph-attestation-flow.md](../vision/glyph-attestation-flow.md)
**Depends on:** Multi-glyph meld (complete), Watcher engine (complete), Python plugin attest() (complete)

Attestations flow through meld compositions. The meld edge is both spatial grouping and reactive data pipeline. AX is always live. Subscriptions compile on meld.

---

## Phase 1: Trace the existing reactive loop

Before building anything, verify the full path attestations already travel. The AX glyph already registers a watcher via WebSocket, the watcher engine already matches and broadcasts back. Understanding exactly where this loop stops is the foundation.

### 1.1 Trace AX → watcher → broadcast → frontend

- [x] Confirm AX glyph sends `watcher_upsert` on query change
  - `web/ts/components/glyph/ax-glyph.ts:177-183` — sends `watcher_upsert` via WebSocket with `watcher_id: ax-glyph-{glyphId}`

- [x] Confirm server creates watcher from WebSocket message
  - `server/client.go:1025-1148` — `handleWatcherUpsert()` creates `storage.Watcher` with `AxQuery` field, persists to DB, calls `ReloadWatchers()`

- [x] Confirm watcher engine parses AX query into filter
  - `ats/watcher/engine.go:112-154` — `loadWatchers()` calls `parser.ParseAxCommandWithContext()` on `w.AxQuery`, merges into `w.Filter`

- [x] Confirm `OnAttestationCreated` matches and broadcasts
  - `ats/watcher/engine.go:288-340` — iterates watchers, calls `matchesFilter()`, then `broadcastMatch()` callback
  - `server/watcher_handlers.go:352-379` — broadcasts as `watcher_match` message type

- [x] Confirm frontend receives and routes `watcher_match`
  - `web/ts/websocket.ts:161-179` — extracts glyph ID from `ax-glyph-{id}` prefix, calls `updateAxGlyphResults()`

### 1.2 Trace watcher action dispatch

- [x] Understand current `executeAction` dispatch
  - `ats/watcher/engine.go:383-414` — switches on `ActionType`: python, webhook, llm_prompt
  - `ats/watcher/engine.go:416-461` — `executePython()`: injects attestation as JSON string prepended to Python code, POSTs to `/api/python/execute`
  - Note: AX glyph watchers have `ActionData: ""` (empty), `ActionType: python` — they broadcast matches but don't execute code

- [x] Map the gap: broadcast exists, action execution doesn't trigger for AX glyph watchers
  - The watcher engine has two paths: `broadcastMatch` (always, for UI) and `executeAction` (gated by `MaxFiresPerMinute > 0`)
  - AX glyph watchers use broadcast only — no `glyph_execute` action type exists yet

### 1.3 Trace composition edge availability

- [x] Confirm composition edges reach the backend
  - `web/ts/components/glyph/meld/meld-composition.ts:131` → `addComposition()` → `POST /api/canvas/compositions`
  - `glyph/handlers/canvas.go:172-186` — `handleUpsertComposition()` decodes and delegates to `canvas_store.go:205-262` (transactional insert)
  - `db/sqlite/migrations/021_dag_composition_edges.sql` — `composition_edges` table: `(composition_id, from_glyph_id, to_glyph_id, direction, position)`

- [x] Confirm edge data includes glyph types (or can be resolved)
  - Edges store glyph IDs only, not glyph types
  - Glyph type stored in `canvas_glyphs.symbol` column (`db/sqlite/migrations/019_create_canvas_state_tables.sql`)

### 1.4 Map the python execution request shape

- [x] Document current `/api/python/execute` request
  - Request body: `{"code": string}` — no glyph ID, no upstream attestation, no composition context
  - Watcher engine calls it at `ats/watcher/engine.go:442`

- [x] Document `attest()` actor field
  - `qntx-python/src/atsstore.rs:181-213` — `actors: Option<Vec<String>>`, defaults to empty via `unwrap_or_default()`
  - No glyph identity injected — attestations from `attest()` have no actor unless user explicitly passes one

**Manual verification for Phase 1:**
Create an AX glyph with query `contact`, create an attestation via CLI `as contact alice`, confirm AX glyph updates in real-time. Then meld ax→py, query `composition_edges` table to confirm edges are stored. This proves the two halves (reactive broadcast + composition storage) both work — they just aren't connected.

---

## Phase 2: Composition-aware subscriptions

The core: when a meld edge is created, compile it into a subscription that the watcher engine can evaluate. This is the beefiest piece — it connects the reactive broadcast system to the composition DAG.

### 2.1 New action type: `glyph_execute`

Add a watcher action type that executes a specific canvas glyph with an injected attestation, rather than running inline Python code.

- [x] Add `ActionTypeGlyphExecute` to `ats/storage/watcher_store.go`
  - `ActionTypeGlyphExecute = "glyph_execute"` constant alongside python/webhook/llm_prompt

- [x] Add `executeGlyph()` to `ats/watcher/engine.go` alongside `executePython()`/`executeWebhook()`
  - Parses `GlyphExecuteAction` from `ActionData` JSON: `{target_glyph_id, target_glyph_type, composition_id, source_glyph_id}`
  - For py glyph: fetches code from canvas store, calls `/api/python/execute` with `upstream_attestation`
  - For prompt glyph: fetches template from canvas store, calls `/api/prompt/direct` with `upstream_attestation`
  - Broadcasts `glyph_fired` WebSocket message: started → success/error

- [x] Wire into `executeAction()` switch
  - `engine.go` switch case dispatches `ActionTypeGlyphExecute` → `executeGlyph()`

### 2.2 Compile meld edges to subscriptions

When the backend receives a composition upsert (POST `/api/canvas/compositions`), compile each `right`-direction edge into a watcher subscription.

- [x] Add `compileSubscriptions()` to `glyph/handlers/canvas.go`
  - Iterates composition edges, filters `direction == "right"`
  - Resolves source and target glyph types via canvas store
  - AX source: clones `AxQuery` from existing `ax-glyph-{fromId}` watcher
  - Py/Prompt source: sets `Filter.Actors = ["glyph:{fromId}"]`
  - Target must be executable (`py` or `prompt`)

- [x] Register compiled subscriptions with watcher engine
  - Watcher ID: `meld-edge-{compositionId}-{fromId}-{toId}`
  - Uses `CreateOrReplace` (INSERT OR REPLACE) for idempotent upserts — concurrent composition position updates would race with DeleteByPrefix+Create
  - Calls `engine.ReloadWatchers()` after registration

- [x] Deregister on composition delete
  - `handleDeleteComposition` deletes all `meld-edge-{compositionId}-*` watchers via `DeleteByPrefix`
  - Reloads watchers after deletion

- [ ] Deregister stale edges on composition update
  - Currently `compileSubscriptions` only creates/replaces — removed edges leave orphan watchers
  - Need: diff old vs new edges, delete watchers for removed edges

- [x] Handle AX edge compilation: reuse existing watcher filter
  - Looks up `ax-glyph-{fromId}` watcher, clones its `AxQuery` field
  - Logs warning and skips if AX watcher not found

### 2.3 Actor convention for py/prompt glyphs

For producer→downstream edges to work, attestations created by py/prompt must carry `actor: glyph:{glyphId}`.

- [x] Pass glyph ID into python execution request
  - `py-glyph.ts:95-99` sends `glyph_id: glyph.id` in POST body
  - `qntx-python/src/handlers.rs:58` parses `glyph_id` from request, sets thread-local

- [x] Inject glyph ID as default actor in `attest()`
  - Option A (chosen): Rust side — `atsstore.rs` thread-local `CURRENT_GLYPH_ID`
  - `attest()` defaults actors to `["glyph:{id}"]` when glyph_id is set and user didn't pass explicit actors

- [x] Same for prompt handler result attestations
  - `PromptDirectRequest` has `glyph_id` field (`server/prompt_handlers.go:78`)
  - `executeGlyphPrompt` (`engine.go`) already sends `glyph_id` in request body
  - Prompt handler doesn't create attestations yet (Phase 2.4); field is ready for when it does

### 2.4 Upstream attestation injection

The downstream glyph needs the triggering attestation.

- [x] Add `upstream_attestation` field to `/api/python/execute` request
  - `qntx-python/src/handlers.rs:62` — `upstream_attestation: Option<serde_json::Value>` in ExecuteRequest
  - Threaded through `execute_with_ats` → `execute_inner` as new parameter
  - `ats/watcher/engine.go:534` — sends `upstream_attestation: json.RawMessage` in request body (replaces code-prepend hack)

- [x] Bump qntx-python-plugin version 0.4.2 → 0.5.0 (new `upstream_attestation` field + actor convention)

- [x] Inject `upstream` in Python runtime
  - `qntx-python/src/execution.rs:135-152` — after `inject_attest_function()`, uses `json.loads()` to convert attestation JSON to Python dict
  - When present: `upstream = {"id": "...", "subjects": [...], ...}`
  - When absent: `upstream = None` (always available as global)

- [x] Inject attestation into prompt template resolution
  - Server-side approach: `server/prompt_handlers.go:79` — `UpstreamAttestation *types.As` on `PromptDirectRequest`
  - `HandlePromptDirect` parses template with `prompt.Parse()`, interpolates `{{field}}` placeholders from upstream attestation
  - `ats/watcher/engine.go:572` — `executeGlyphPrompt` sends `upstream_attestation` in request body

**Manual verification for Phase 2:**
1. Meld [ax: contact] → [py] where py code is `print(upstream.subjects)`. Query `watchers` table — confirm `meld-edge-*` watcher exists.
2. Run `as contact bob` from CLI. Confirm py glyph fires automatically, result shows `['bob']`.
3. In py glyph, call `attest(subjects=upstream.subjects, predicates=["enriched"])`. Confirm attestation has `actor: glyph:{py_glyph_id}`.
4. Meld another py glyph to the right: [ax] → [py-A] → [py-B]. Run `as contact carol`. Confirm py-A fires, then py-B fires with py-A's attestation.

---

## Phase 3: Edge cursor + deduplication

Prevent reprocessing on restart and handle edge cases.

- [x] New migration: `composition_edge_cursors` table
  - `db/sqlite/migrations/023_composition_edge_cursors.sql`
  - **Gotcha:** Go's `//go:embed` caches migration files at build time. Adding migration 023 required `go clean -cache` to force re-embedding. Symptom: `total_migrations=22` in startup logs even after adding migration file.

- [x] Update cursor after successful `executeGlyph()` in watcher engine
  - `ats/watcher/engine.go:460` — `updateEdgeCursor()` upserts cursor from `GlyphExecuteAction` fields
  - `GlyphExecuteAction` now includes `composition_id` and `source_glyph_id` alongside target fields

- [x] On `ReloadWatchers()` / server restart: apply cursor as `TimeStart` filter on meld edge watchers
  - `ats/watcher/engine.go:439` — `applyEdgeCursor()` queries cursor table, sets `w.Filter.TimeStart`
  - Called during `loadWatchers()` for each `glyph_execute` watcher

- [x] On composition delete: cascade delete cursors
  - `glyph/handlers/canvas.go:234` — deletes from `composition_edge_cursors` before deleting composition

**Manual verification:** Meld ax→py, fire some attestations, restart server, fire more — confirm no duplicates.

---

## Phase 4: Frontend lifecycle + feedback

Wire up the UI to show subscription state and glyph execution feedback.

### 4.1 `glyph_fired` WebSocket message (complete)

- [x] Proto-defined shared type: `GlyphFired` in `glyph/proto/events.proto`
  - Fields: `glyph_id`, `attestation_id`, `status` ("started"/"success"/"error"), `error`, `timestamp`
  - Go struct `GlyphFiredMessage` in `server/types.go` with matching JSON tags
  - TypeScript type generated from proto + extended with `type: 'glyph_fired'` in `web/types/websocket.ts`

- [x] Server broadcasts `glyph_fired` in `executeGlyph()` — started before execution, success/error after
  - `server/watcher_handlers.go:418-443` — `broadcastGlyphFired()` creates message, sends to broadcast hub
  - `server/broadcast.go` — `processBroadcastRequest` routes `glyph_fired` to `sendMessageToClients`

- [x] Frontend handler in `web/ts/websocket.ts` MESSAGE_HANDLERS
  - Finds glyph DOM element by `data-glyph-id`, sets `data-execution-state` attribute
  - Maps status: started→running, success→completed, error→failed
  - Auto-clears "completed" after 3s
  - Logs warning if DOM element not found (debugging aid)

- [x] CSS-driven visual feedback in `web/css/glyph/base.css`
  - `.canvas-glyph[data-execution-state="running"]` → blue border (`--glyph-status-running-text`)
  - `.canvas-glyph[data-execution-state="completed"]` → green border (`--glyph-status-success-text`)
  - `.canvas-glyph[data-execution-state="failed"]` → red border (`--glyph-status-error-text`)
  - **Finding:** Needed `:not(.canvas-error-glyph)` qualifier for specificity 0,3,0 to override `canvas.css` sync-state border-color (0,2,0). Also needed inline `transition: border-color 0.2s ease` to override canvas.css's 1.5s mode transition.

### 4.2 Bugs found and fixed during Phase 4

- [x] **IPv6 connection refused** — macOS resolves `localhost` to `[::1]` (IPv6). Server binds all interfaces but HTTP client tried IPv6 first.
  - Fix: `server/watcher_handlers.go` changed `localhost` → `127.0.0.1`

- [x] **Hardcoded port 877** — `executeGlyphPython/Prompt` used hardcoded port instead of configured port.
  - Fix: `server/watcher_handlers.go` uses `am.GetServerPort()` for dynamic port resolution

- [x] **Race condition on concurrent composition updates** — Dragging a melded composition triggers multiple position-update upserts, each calling `compileSubscriptions`. `DeleteByPrefix` + `Create` raced.
  - Fix: Added `CreateOrReplace` method to `WatcherStore` (INSERT OR REPLACE INTO), eliminated `DeleteByPrefix` during compile

- [x] **Broadcast hub missing `glyph_fired` case** — `processBroadcastRequest` switch in `broadcast.go` didn't handle `glyph_fired`
  - Fix: Added `case "glyph_fired": s.sendMessageToClients(req.payload, req.clientID)`

- [x] **Go build cache stale after adding migration 023** — `//go:embed` baked in 22 migrations, didn't pick up new file
  - Fix: `go clean -cache` forces re-embed on next build

- [x] **Debug logs invisible at Info level** — Rate limit and `MaxFiresPerMinute=0` checks used `Debugw`, suppressed in dev
  - Temporary fix: Upgraded key diagnostic logs to `Infow` (should revert to `Debugw` after debugging)

### 4.3 Not yet implemented

- [ ] "Watching" indicator when glyph is downstream in a meld
  - On meld: glyph detects it has an incoming `right` edge, shows subtle listening state
  - On unmeld: revert to standalone appearance

- [ ] Result glyph auto-meld from subscription-triggered execution
  - Server-side execution creates results, but result glyph creation is client-side (`py-glyph.ts:232`)
  - Need: server returns result data via `glyph_fired` success message, frontend creates result glyph

- [ ] Rate limit feedback
  - When a meld edge subscription is rate-limited, broadcast to frontend so user sees it

**Manual verification:** Meld ax→py, create attestation via TS glyph, watch py glyph border flash green on execution. Confirmed working.

---

## Phase 5: Edge cases + hardening

### 5.1 Frontend routing for meld-edge watcher matches

- [ ] `watcher_match` handler only recognizes `ax-glyph-` prefix
  - Meld-edge watchers broadcast `watcher_match` messages with IDs like `meld-edge-melded-ax-{...}-py-{...}`
  - Frontend logs: `Received watcher_match with unexpected watcher_id format: meld-edge-...`
  - Fix: extend `watcher_match` handler to also route meld-edge matches (extract composition/glyph IDs from the watcher ID, deliver attestation to appropriate glyph)

### 5.2 Stale edge cleanup on composition update

- [ ] `compileSubscriptions` only creates/replaces watchers — doesn't delete watchers for edges that were removed
  - When a glyph is unmelded from a composition, the old edge's watcher persists as orphan
  - Fix: before compiling, query existing `meld-edge-{compositionId}-*` watchers, diff against current edges, delete stale ones

### 5.3 AX query change propagation

- [ ] When user edits AX glyph query, recompile all meld edge subscriptions sourced from that AX glyph
  - Hook into debounced save at `ax-glyph.ts:236-246`
  - Backend: on AX watcher update, find all `meld-edge-*-{axGlyphId}-*` watchers, update their `AxQuery` field
  - Or: frontend re-POSTs the composition on AX query change, triggering `compileSubscriptions`

### 5.4 Composition reconstruction on page load

- [ ] `canvas-glyph.ts:417-469` reconstructs melded DOM on page load but doesn't verify subscriptions
  - Subscriptions may have been created in a previous session and still exist in the watchers table
  - On load: for each composition, call `compileSubscriptions` to ensure meld edge watchers exist
  - Idempotent thanks to `CreateOrReplace` — safe to call repeatedly

### 5.5 Unmeld cleanup

- [ ] `meld-composition.ts` decompose flow: when glyphs are pulled apart, DELETE the meld edge watcher
  - Also delete edge cursor for that edge
  - Frontend should call a delete endpoint, or the next composition upsert (with the glyph removed) should trigger stale edge cleanup (5.2)

### 5.6 Multiple downstream glyphs from same source

- [ ] One AX glyph melded to py-A on right AND py-B (if supported by future port rules)
  - Each edge compiles to its own subscription — both fire independently
  - Already works architecturally (each edge = separate watcher), needs testing

### 5.7 Error propagation through the DAG

- [ ] If py glyph execution fails, should downstream glyphs be notified?
  - Currently: `glyph_fired` with `status: "error"` is broadcast, downstream glyphs don't react
  - Minimum: failed glyph shows red border (already works via CSS)
  - Future: downstream glyphs could show "upstream failed" indicator

### 5.8 Diagnostic log cleanup

- [ ] Revert temporary `Infow` diagnostic logs back to `Debugw` in `ats/watcher/engine.go`
  - Rate limit checks, `MaxFiresPerMinute=0` skip logs, `executeAction` entry log
  - These were upgraded for debugging during Phase 4; should return to Debug level for production

**Manual verification:** Edit AX query while melded → downstream subscription updates. Refresh page → subscriptions still fire. Unmeld → subscription stops. Pull apart melded composition → orphan watchers cleaned up.
