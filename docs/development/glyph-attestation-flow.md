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

- [ ] Add `ActionTypeGlyphExecute` to `ats/storage/watcher_store.go:16-20`
- [ ] Add `executeGlyph()` to `ats/watcher/engine.go` alongside `executePython()`/`executeWebhook()`
  - Takes: target glyph ID, glyph type, attestation
  - For py glyph: calls `/api/python/execute` with attestation in request body (new field, see 2.4)
  - For prompt glyph: calls `/api/prompt/direct` with attestation in request body (new field)
  - Broadcast `glyph_fired` WebSocket message to frontend (new message type)
- [ ] Wire into `executeAction()` switch at `engine.go:386`

### 2.2 Compile meld edges to subscriptions

When the backend receives a composition upsert (POST `/api/canvas/compositions`), compile each `right`-direction edge into a watcher subscription.

- [ ] Add `CompileSubscriptions()` to `glyph/handlers/canvas.go` or new `glyph/subscription/compiler.go`
  - Input: composition edges + glyph metadata (type per glyph ID)
  - For each `right` edge:
    - Resolve source glyph type from `canvas_glyphs` table
    - **AX source**: watcher filter = AX glyph's query (from `watchers` table, keyed by `ax-glyph-{fromId}`)
    - **Py/Prompt source**: watcher filter = `{actors: ["glyph:{fromId}"]}`
  - Output: watcher definition per edge with `ActionType: glyph_execute`, `ActionData: {target_glyph_id, target_glyph_type}`

- [ ] Register compiled subscriptions with watcher engine
  - Use watcher ID convention: `meld-edge-{compositionId}-{fromId}-{toId}`
  - Create/update via `WatcherStore.Create()` / `WatcherStore.Update()`
  - Call `engine.ReloadWatchers()` after registration

- [ ] Deregister on composition delete or edge removal
  - On composition DELETE: remove all `meld-edge-{compositionId}-*` watchers
  - On composition UPDATE: diff old vs new edges, remove stale watchers, create new ones
  - `handleUpsertComposition` at `canvas.go:172` needs pre-update edge diffing

- [ ] Handle AX edge compilation: reuse existing watcher filter
  - AX glyph already has a watcher `ax-glyph-{glyphId}` with parsed filter
  - Meld edge subscription can clone that filter (or reference the same AX query string)
  - When AX query changes (debounce update), recompile affected meld edge subscriptions

### 2.3 Actor convention for py/prompt glyphs

For producer→downstream edges to work, attestations created by py/prompt must carry `actor: glyph:{glyphId}`.

- [ ] Pass glyph ID into python execution request
  - Add `glyph_id` field to `/api/python/execute` request body
  - Frontend `py-glyph.ts:95-101` sends glyph ID in POST
  - Python plugin HTTP handler passes it through to execution context

- [ ] Inject glyph ID as default actor in `attest()`
  - Option A: Rust side (`atsstore.rs`) — if actors is empty and glyph_id is set, default to `["glyph:{glyph_id}"]`
  - Option B: Python side — inject `_glyph_id` global, let user override but provide default
  - Prefer Option A: transparent, user doesn't need to know

- [ ] Same for prompt handler result attestations
  - When prompt executes via `glyph_execute` action, pass glyph ID through
  - `createResultAttestation()` at `ats/so/actions/prompt/handler.go:352` uses glyph ID as actor

### 2.4 Upstream attestation injection

The downstream glyph needs the triggering attestation.

- [ ] Add `upstream_attestation` field to `/api/python/execute` request
  - JSON attestation object, optional (null for standalone execution)
  - Rust plugin deserializes and injects as Python global `upstream`

- [ ] Inject `upstream` in Python runtime
  - `qntx-python/src/execution.rs` — after `inject_attest_function()`, inject upstream dict
  - When present: `upstream = {"id": "...", "subjects": [...], ...}`
  - When absent: `upstream = None`

- [ ] Inject attestation into prompt template resolution
  - `prompt-glyph.ts:186-197` — replace "coming soon" error with actual resolution
  - When glyph receives attestation via `glyph_fired` WebSocket message, interpolate `{{variables}}` from attestation fields
  - Alternatively: prompt execution happens server-side in `executeGlyph()`, template interpolation in Go

**Manual verification for Phase 2:**
1. Meld [ax: contact] → [py] where py code is `print(upstream.subjects)`. Query `watchers` table — confirm `meld-edge-*` watcher exists.
2. Run `as contact bob` from CLI. Confirm py glyph fires automatically, result shows `['bob']`.
3. In py glyph, call `attest(subjects=upstream.subjects, predicates=["enriched"])`. Confirm attestation has `actor: glyph:{py_glyph_id}`.
4. Meld another py glyph to the right: [ax] → [py-A] → [py-B]. Run `as contact carol`. Confirm py-A fires, then py-B fires with py-A's attestation.

---

## Phase 3: Edge cursor + deduplication

Prevent reprocessing on restart and handle edge cases.

- [ ] New migration: `composition_edge_cursors` table
  ```sql
  CREATE TABLE composition_edge_cursors (
      composition_id TEXT NOT NULL,
      from_glyph_id TEXT NOT NULL,
      to_glyph_id TEXT NOT NULL,
      last_processed_id TEXT NOT NULL,
      last_processed_at DATETIME NOT NULL,
      PRIMARY KEY (composition_id, from_glyph_id, to_glyph_id)
  );
  ```
- [ ] Update cursor after successful `executeGlyph()` in watcher engine
- [ ] On `ReloadWatchers()` / server restart: apply cursor as `TimeStart` filter on meld edge watchers
- [ ] On composition delete: cascade delete cursors

**Manual verification:** Meld ax→py, fire some attestations, restart server, fire more — confirm no duplicates.

---

## Phase 4: Frontend lifecycle + feedback

Wire up the UI to show subscription state and glyph execution feedback.

- [ ] New WebSocket message type: `glyph_fired`
  - Payload: `{glyph_id, attestation_id, status: "started"|"success"|"error", result?}`
  - `web/ts/websocket.ts` — add handler alongside `watcher_match`
  - Glyph element receives visual state update (border flash, status indicator)

- [ ] Downstream glyph shows "watching" indicator when in meld with upstream
  - On meld: glyph detects it has an incoming `right` edge, shows subtle listening state
  - On unmeld: revert to standalone appearance

- [ ] Result glyph auto-meld from subscription-triggered execution
  - When `executeGlyph()` runs py code, result glyph creation + auto-meld needs to work
  - Today result glyph is created client-side (`py-glyph.ts:232`); subscription execution is server-side
  - Options: server returns result via `glyph_fired` message, frontend creates result glyph from that

- [ ] Rate limit feedback
  - When a meld edge subscription is rate-limited, broadcast to frontend so user sees it

**Manual verification:** Meld ax→py, create attestation, watch py glyph visually indicate firing, see result appear below.

---

## Phase 5: Edge cases + hardening

- [ ] AX query change propagation
  - When user edits AX glyph query, recompile all meld edge subscriptions sourced from that AX glyph
  - Hook into debounced save at `ax-glyph.ts:236-246`

- [ ] Composition reconstruction on page load
  - `canvas-glyph.ts:417-469` — reconstructs melded DOM. Also needs to verify subscriptions are registered with watcher engine (may have been created in a previous session)
  - On load: for each composition, ensure meld edge watchers exist; create if missing

- [ ] Unmeld cleanup
  - `meld-composition.ts` decompose flow: when glyphs are pulled apart, DELETE the meld edge watcher
  - Also delete edge cursor for that edge

- [ ] Multiple downstream glyphs from same source
  - One ax glyph melded to py-A on right AND py-B (if supported by future port rules)
  - Each edge compiles to its own subscription — both fire independently

- [ ] Error propagation through the DAG
  - If py glyph execution fails, should downstream glyphs be notified?
  - Minimum: `glyph_fired` message with `status: "error"` so downstream UI knows

**Manual verification:** Edit AX query while melded → downstream subscription updates. Refresh page → subscriptions still fire. Unmeld → subscription stops.
