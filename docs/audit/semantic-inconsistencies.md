# Semantic Inconsistency Audit

Functions that grew in responsibility and no longer reflect what their names describe.

Verified against source. Sorted by severity within each language.

---

## Go

### HIGH — `handleUpsertComposition` orchestrates watcher subscriptions

**`glyph/handlers/canvas.go:207`**

Name says "upsert composition" — store it and return. Implementation also compiles meld edges into watcher subscriptions via `compileSubscriptions()` (line 222). The storage call is the minor part; the watcher engine orchestration is the real work.

### ~~HIGH~~ RESOLVED — `handleDeleteComposition` → `deleteMeldComposition`

**`glyph/handlers/canvas.go:235`**

Compositions exist for meld wiring. Teardown (watchers, cursors, SE state) is inherent to what a meld composition is — not a hidden side effect. Renamed to `deleteMeldComposition`.

### HIGH — `postReload` runs compound watcher suppression and historical query dispatch

**`server/watcher_reload.go:111`**

Name says "post-reload" — cleanup after a watcher reload. Implementation checks if SE watchers are compound-suppressed, propagates semantic queries to compound watchers, persists updated thresholds, and spawns goroutines for historical query dispatch. This is compound watcher orchestration, not post-reload cleanup.

### HIGH — `NewQNTXServer` is a full initialization orchestrator

**`server/init.go:53`**

`New` prefix in Go means "construct and return." This function creates loggers, loads config, creates dependencies, validates them, creates cancellation context, configures daemon with worker pool, creates ticker, initializes WebAuthn, registers type definitions, initializes plugin registry, starts gRPC services, registers LLM providers, initializes watcher engine, sets up canvas handlers, configures embedding service, sets up sync, and watches config files. It is the server bootstrap sequence, not a constructor.

### MEDIUM — `initViper` loads and merges all config sources

**`am/load.go:86`**

Name says "init Viper." Implementation also binds environment variables, sets defaults, merges config files from three locations (system, user, project) in precedence order, tracks config sources, and caches the result globally. This is the full config bootstrap — `bootstrapConfig` would be more accurate.

### MEDIUM — `startBackgroundServices` reads state and makes conditional decisions

**`server/lifecycle.go:41`**

Name says "start background services." Implementation first reads daemon state from database, decides whether to enable the daemon based on saved state, *then* starts services conditionally. It is "restore-and-start," not just "start."

### MEDIUM — `executeAction` handles retry queuing and cursor updates

**`ats/watcher/engine_execute.go:18`**

Name says "execute action." On failure, it also enqueues for retry (persists to queue) and updates edge cursors for meld watchers. The retry and cursor side effects are invisible from the name.

### MEDIUM — `UpdateEmbeddingsEnabled` validates model files and rotates backups

**`am/persist.go:148`**

Name says "update embeddings enabled flag." Implementation validates that the model file exists at the configured path before enabling, then writes the UI config file with backup rotation. A flag toggle that can fail validation and rotates backups.

---

## TypeScript

### HIGH — `makeDraggable` is a glyph composition orchestrator

**`web/ts/components/glyph/glyph-interaction.ts:241`**

Name says "make draggable." Implementation (~360 lines) also handles multi-glyph selection dragging, meld proximity detection with visual feedback, meld composition creation via `performMeld`, position persistence to `uiState.addCanvasGlyph()`, backend sync via canvas sync queue, z-index management, and window manifestation checks. "Draggable" is the trigger; the function orchestrates glyph composition interactions.

### HIGH — `reportHttpSuccess` / `reportHttpFailure` track reachability, not success

**`web/ts/connectivity.ts:121,169`**

`reportHttpSuccess` is called on ANY HTTP response including 4xx/5xx. `reportHttpFailure` is called only on network-level fetch failures (TypeError). These track "server responded" vs "server unreachable" — not success vs failure. The names invert what most callers would expect.

### MEDIUM — `makeResizable` also persists to state and syncs to backend

**`web/ts/components/glyph/glyph-interaction.ts:635`**

Name says "make resizable." On mouseup, also persists final dimensions to `uiState.addCanvasGlyph()` which triggers canvas sync to backend. The resize handle is the input; state persistence is the hidden side effect.

### MEDIUM — `isDevMode` exists in two files with different implementations

**`web/ts/dev-mode.ts:48`** — async, calls backend `/api/dev`
**`web/ts/logger.ts`** — checks browser environment (localhost, `__DEV__`)

Same function name, different semantics. Callers importing from the wrong module get silently wrong answers.

### MEDIUM — `setStorageItem` / `removeStorageItem` hide async fire-and-forget IndexedDB writes

**`web/ts/indexeddb-storage.ts:123,146`**

Synchronous function signatures matching `localStorage` API. Implementation writes to in-memory cache synchronously, then fires async IndexedDB writes that can silently fail (errors are caught and logged/toasted, not propagated). Callers assume synchronous persistence semantics.

### MEDIUM — `dispatchSearch` orchestrates three search strategies

**`web/ts/system-drawer.ts:110`**

Name says "dispatch search" — send a search request somewhere. Implementation runs local results synchronously, fires WASM rich search against IndexedDB, and conditionally sends a server search message for semantic enrichment. Three independent search pipelines with different latency profiles, not a single dispatch.

### LOW — `createFollowUpZone` wires full interactive form lifecycle

**`web/ts/components/glyph/glyph-followup.ts:82`**

Name says "create zone" (DOM factory). Implementation also wires event handlers, manages form state, coordinates submission logic, and dispatches API calls via `defaultExecute()`. It is a full interactive component constructor.

### LOW — `handleParseResponse` mutates window global

**`web/ts/ats-semantic-tokens-client.ts:58`**

Name says "handle parse response" — process a message. Implementation also sets `window.atsParseState` (line 72), a global side effect invisible from the function signature or name.

---

## Rust

### HIGH — `cartesian_count` excludes actors; `expand_cartesian` includes them

**`crates/qntx-core/src/attestation/types.rs:68`** — S x P x C (no actors)
**`crates/qntx-core/src/expand.rs:56`** — S x P x C x A (includes actors)

Both use the word "cartesian" but compute different products. `cartesian_count` undercounts relative to the actual expansion. Callers using `cartesian_count` to pre-allocate or estimate will get wrong numbers.

### HIGH — `rebuild_index` is a full replacement, not a rebuild

**`crates/qntx-core/src/fuzzy/engine.rs:112`**

"Rebuild" implies incremental update of an existing index. Implementation replaces all four vocabularies wholesale (subjects, predicates, contexts, actors), deduplicates, sorts, pre-computes lowercase versions, and recomputes the hash. This is `replace_index` or `set_vocabularies`.

### MEDIUM — `classify` is a full conflict analysis pipeline

**`crates/qntx-core/src/classify/classifier.rs:81`**

Name says "classify" — assign categories. Implementation also aggregates `auto_resolved`, `review_required`, and `total_analyzed` counters, filters single-claim groups, and returns a structured analysis output. It is `analyze_conflicts`, not just classification.

### MEDIUM — `has_multiple_dimensions` ignores the actors dimension

**`crates/qntx-core/src/attestation/types.rs:62`**

Checks subjects, predicates, and contexts but not actors. Same gap as `cartesian_count`. Either the name should be `has_multiple_claim_dimensions` or actors should be checked.

### LOW — `total_analyzed` counter skips single-claim groups

**`crates/qntx-core/src/classify/classifier.rs:85`**

`total_analyzed` sounds like it counts all groups analyzed. It only counts groups with 2+ claims. Single-claim groups are silently skipped. `conflict_groups_analyzed` would be accurate.

### LOW — `size()` on MerkleTree returns total leaves, not group count

**`crates/qntx-core/src/sync/merkle.rs:139`**

`size()` returns sum of all leaves across all groups. `group_count()` returns number of groups. `size` is ambiguous — `leaf_count` or `total_attestations` would be clearer.

### MEDIUM — `toggleColorMode` re-renders all visible stream instances

**`web/ts/components/glyph/response-glyph.ts:145`**

Name says "toggle color mode" — flip a flag and return. Implementation also iterates all visible `StreamInstance`s and re-renders their token spans. A toggle with a global repaint side effect.

### MEDIUM — `createTokenPopup` is a stateful component manager

**`web/ts/components/glyph/token-popup.ts:164`**

Name says "create token popup" — factory. Implementation creates the popup element, a detail sub-popover, a legend overlay, manages 3 independent hide timers (`hideTimer`, `legendTimer`, `detailHideTimer`), handles positioning, and wires mouseenter/mouseleave across all three elements. This is a component lifecycle manager returned as a closure bag.

### MEDIUM — `createMorphAnimation` enforces exclusivity and transaction semantics

**`packages/glyphs/morph-transaction.ts:19`**

Name says "create morph animation" — make an animation. Implementation cancels any existing animation on the element (exclusivity), wraps the animation in a Promise, implements commit (forward-fill styles on finish) and rollback (cancel handler), and cleans up event listeners. This is transaction coordination, not animation creation.

---

## Patterns

Three recurring patterns across all three languages:

1. **Orchestration hiding**: CRUD-named functions (`upsert`, `delete`, `create`) that orchestrate multi-system workflows (watcher engine, state sync, backend persistence). The storage operation is one step in a larger pipeline.

2. **Side-effect concealment**: Functions with value-semantics names (`set`, `report`, `make`) that fire-and-forget to async systems, persist to databases, or mutate global state as invisible side effects.

3. **Dimension drift**: Functions operating on "all dimensions" of an attestation that quietly exclude actors, creating inconsistencies between related functions (`cartesian_count` vs `expand_cartesian`, `has_multiple_dimensions` vs actual dimension count).
