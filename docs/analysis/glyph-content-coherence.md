# Glyph Content Coherence

Glyph *chrome* has been unified. `canvasPlaced()` owns container, layout, drag, resize.
`.glyph-title-bar` was "averaged from six prior implementations" (`web/css/glyph/title-bar.css:1-9`).
`wireExpandToWindow()` replaced "the copy-pasted expand-button click handler that existed in
every glyph file" (`packages/glyphs/expand-to-window.ts:1-7`). `spawnOnCanvas()` replaced the
repeated spawn pattern (`web/ts/components/glyph/spawn-on-canvas.ts:1-8`).

Glyph *content* — everything below the title bar — has had no such pass. §1–§3 inventory what is
duplicated there, what primitive is missing, and what already exists but is unused. §4 is the
breakage that grew in the gaps: a delete path that skips its own teardown, two glyphs that
register no teardown at all, attestation text reaching `innerHTML`, and 31 CSS variables that are
used and never defined. §5 is the work that started and stopped — migrations with no code path,
files scheduled for deletion, capabilities built halfway.

## 1. Four content vocabularies coexist

**A. CSS-class vocabulary.** `.glyph-content`, `.glyph-row`, `.glyph-label`, `.glyph-value`,
`.glyph-section`, `.glyph-section-title`, `.glyph-loading` (`web/css/window.css:241-355`),
built via `innerHTML` templates. Used by the tray/window glyphs: `db-glyph.ts`,
`embeddings-glyph.ts`, `default-glyphs.ts`, `llm-provider-glyph.ts`.

**B. Inline `el()` styles.** Every canvas glyph: `ax`, `se`, `py`, `ts`, `prompt`, `note`,
`result`, `attestation`, `triplet`, `type`, `sigma`, `error`. No shared class names below the
title bar; padding, font, and color are re-declared per call site.

**C. GlyphUI form primitives.** `ui.input()`, `ui.button()`, `ui.statusLine()` →
`.glyph-form-group`, `.glyph-input`, `.glyph-btn` (`web/ts/components/glyph/glyph-ui.ts:120-170`,
`web/css/window.css:292-340`). **No glyph under `web/ts/` calls any of them.** The only consumer
is a plugin module: `qntx-plugins/ix-json/web/glyph-module.ts:40-58`. `pty-glyph` and the in-tree
`py-glyph`/`ts-glyph` take `ui.glyph()` and then hand-roll everything inside it.

**D. Server-rendered HTML.** Plugin glyphs on the `content_url` path fetch HTML from the
server and assign it with `innerHTML`, then re-execute the `<script>` tags it contains
(`plugin-glyph.ts:137-160`). The glyph body is a string built by a Go handler. This is the legacy
half of a two-path split (§5.2).

Two content-area classes also compete: `.glyph-content-area` (ax, se, result, attestation,
triplet, type, sigma, plugin, error) and `.glyph-content` (auth, connectivity, chart, and the
window-manifestation wrappers). `prompt-glyph` and `note-glyph` use neither — they append bare
divs with inline flex styles (`prompt-glyph.ts:320-326`, `note-glyph.ts:174-185`).

## 2. Missing primitives

### 2.1 Result row

Four implementations of "one row in a glyph's result list", all with the same shape —
`padding: 8px`, `marginBottom: 4px`, `borderRadius: 2px`, `cursor: pointer`, class
`ax-glyph-result-item has-tooltip`, `dataset.attestation`, `dataset.tooltip`, `dblclick` →
spawn, inner `11px` monospace text div — differing only in background tint and body:

| | |
|---|---|
| `ax-glyph.ts:302` | `renderAttestation` — `rgba(31, 61, 31, 0.35)` |
| `type-result-line.ts:73` | `renderTypeResultLine` — `rgba(60, 50, 80, 0.35)` |
| `triplet-glyph.ts:474` | `renderTripletResultLine` — `rgba(30, 40, 50, 0.4)` |
| `sigma-glyph.ts:620` | `renderSigmaResultLine` — `rgba(80, 60, 30, 0.35)` |

The row is the unit the AX glyph is made of; it has no name. Every new result kind is a fifth copy.

The ` · ` separated fact list inside those rows is itself repeated (`type-result-line.ts:117-153`,
`sigma-glyph.ts:657-685`, `result-glyph.ts:107-119`) — three hand-built versions of the same
"dim separator between metadata facts".

### 2.2 Key/value row

`renderAttestationAttrs` builds the same key-label + value block four times *inside one
function* (`attestation-attrs.ts:459-516`): `row.style.marginBottom = '4px'`, `keyEl` at
`10px` / `--text-secondary` / `marginBottom: 1px`.

A `.glyph-row` + `.glyph-label` + `.glyph-value` set exists in CSS (`window.css:249-262`) and is
used 10+ times by `embeddings-glyph.ts` and `default-glyphs.ts`. It is a different component:
`display: flex; justify-content: space-between`, a two-column label/value row. The attestation
block stacks the key above the value. Two key/value layouts, one named and one not. The unnamed
one is written four times.

### 2.3 Code-editor glyph

`py-glyph.ts` and `ts-glyph.ts` are near-identical: the same height calculation
(`lineHeight 24`, `titleBarH 36`, `min 120`, `max 600`), the same `▶` titlebar button, the same
CodeMirror dynamic-import boot with `defaultKeymap`/`oneDark`/`lineWrapping`, the same
`createAutoSave` wiring, the same `(element as any).editor` stash, the same "Error loading
editor" fallback, the same sync/connectivity subscriptions.

The execution halves differ and do not belong in one primitive: py posts to
`/execute` and gets an `ExecutionResult` back, ts runs `AsyncFunction` in the page against
`buildQntxApi` (`ts-glyph.ts:57-115`) with direct IndexedDB attest/query rights and no sandbox.
The editor half is what is duplicated: boot, autosave, sizing, state wiring. A third language
means a third copy of that half.

### 2.4 Query-glyph shell

`ax-glyph.ts` and `semantic-glyph.ts` share `query-glyph-states.ts` for *colors, empty state and
error* only. Still duplicated in both: query input embedded in the title bar, the 500 ms debounce,
the `watcher_upsert` enable/disable lifecycle including the disable-on-cleanup copy, the
`rgba(25, 25, 30, 0.95)` results container with `borderTop`, `setupGlyphResizeObserver`, and the
sync/connectivity subscriptions (`ax-glyph.ts:96-290`, `semantic-glyph.ts:208-345`).

### 2.5 Attestation-family shell

`attestation`, `triplet`, `type` and `sigma` glyphs each hand-build: custom title bar → `⬆`
expand button (`titlebar-btn`, `flexShrink: 0`, `marginLeft: auto`, `preventDrag`) → optional
meta pill → `canvasPlaced({ useMinHeight: true, height: hasContent ? N : 28 })` → a
`glyph-content-area` div with an opaque background and `borderTop: 1px solid var(--border)` →
`wireExpandToWindow` whose `renderContent` wraps the same builder in an `outer`/`glyph-content`
pair. See `attestation-glyph.ts:100-160`, `triplet-glyph.ts:386-446`, `type-glyph.ts:165-215`,
`sigma-glyph.ts:498-545`. Even the container background drifted: three use
`rgba(25, 25, 30, 0.95)`, triplet uses `rgba(30, 35, 42, 0.95)`.

### 2.6 Action-symbol vocabulary

`sym` is the source of truth for domain symbols, and glyph content ignores it for *action*
affordances — run, expand, copy, close have no canonical symbol and no shared button factory:

- copy is `⎘` (U+2398) in `result-glyph.ts:335` and `📋` (emoji) in `error-glyph.ts:111` —
  different alphabets for the same action
- dismiss is `✕` (U+2715) in `error-glyph.ts:146` but `×` (U+00D7) in `note-glyph.ts:123` and
  `title-bar-controls.ts:38`
- the same character is spelled as a literal in one file and an escape in another: run is
  `'▶'` in `py-glyph.ts:68` and `prompt-glyph.ts:178` but `'\u25B6'` in `ts-glyph.ts:133`;
  copy is `'⎘'` in `result-glyph.ts:335` but `'\u2398'` in `result-glyph.ts:1004`
- `SO` (`⟶`) is imported from `@generated/sym.js` in `prompt-glyph.ts` but hardcoded as a
  literal in `error-glyph.ts:124,135` and `canvas/action-bar.ts:66`
- `"Expand to window"` is retyped as a `title` string in six files

`result-glyph.ts:314-318` has the primitive already — a three-line `headerBtn(label, title)` —
declared inside a function body, invisible to every other glyph.

### 2.7 Glyph identity palette

Four private palettes, none in `tokens.css`, none on the registry entry:

- `sigma-glyph.ts:21-25` — `AMBER`, `AMBER_DIM`, `AMBER_VALUE`, `AMBER_BAR`, `AMBER_BAR_BG`
- `triplet-glyph.ts:24-28` — `TRIPLET`, `TRIPLET_KEYWORD`, `TRIPLET_VALUE`, `TRIPLET_DIM`, `TRIPLET_BG`
- `type-glyph.ts:22-24` — `TYPE_COLOR`, `TYPE_DIM`, `TYPE_VALUE`
- `attestation-attrs.ts:18-21` — `AZURE`, `AZURE_KEYWORD`, `AZURE_VALUE`, `AZURE_BORDER`
- plus `thread-glyph.ts:31-39` — eight literal reds

All four converge on the same three roles (accent / value / dim), which is a
`{ accent, value, dim, bg }` tuple per symbol. `GlyphTypeEntry` (`glyph-registry.ts:28-46`)
carries symbol, className, title, label, render — but no color, so `py` (`#2a5578`/`#FFD43B`)
and `ts` (`#5c3d1a`/`#f0c878`) pass their identity as literals to `ui.glyph()` instead.

### 2.8 Empty / loading / error inside content

- `appendEmptyState()` is scoped to query glyphs and takes a per-glyph class name
  (`query-glyph-states.ts:37-46`); callers pass `'ax-glyph-empty-state'` /
  `'se-glyph-empty-state'` and then `querySelector` it back out by that string
  (`ax-glyph.ts:349`, `semantic-glyph.ts:480`).
- Elsewhere empty is ad-hoc text: `'(no output)'` (`result-glyph.ts:263`),
  `'No data available'` (`chart-glyph.ts:178`), `'No document loaded'` (`doc-glyph.ts:72`),
  an inline-styled div in `db-glyph.ts:252,338`.
- Loading is `'searching...'` inline-styled (`ax-glyph.ts:134-141`), `.glyph-loading`
  (`chart-glyph.ts:134`), `'Loading...'` (`plugin-glyph.ts:94`).
- `createLoadingState()` / `createErrorState()` / `createRichErrorState()` /
  `parseError()` exist in `base-panel-error.ts:57-170` and are used by panels only.
  `UI_TEXT.NO_DATA` / `UI_TEXT.LOADING` (`config.ts:6-14`) are used by neither.

### 2.9 Button-with-error-surface

`web/CLAUDE.md` makes `Button` the sanctioned error surface: "Throws from `onClick` → automatic
slide-out error display", with `alert`/`confirm`/`toast` ESLint-banned. Inside
`components/glyph/` there are **28 raw `<button>` creations and 1 `new Button(...)`**
(`manifestations/canvas-expanded.ts`). So the mandated error affordance does not exist in glyph
content: a failing action inside a glyph currently surfaces as a `log.error` (`py-glyph.ts:112`),
a spawned error result (`py-glyph.ts:114-121`), a tinted status section
(`prompt-glyph.ts:132-175`), a red text block (`query-glyph-states.ts:52-96`), or a whole error
glyph (`error-glyph.ts`) — five presentations of "that didn't work".

### 2.10 Tooltip

Two hover systems inside glyph content: the terminal-styled manager
(`has-tooltip` + `data-tooltip`, 300 ms delay, `components/tooltip.ts`) at 39 sites, and native
`title=` at 15 sites in `components/glyph/`. The same control differs by file — `prompt-glyph.ts:179`
gives the run button `titlebar-btn has-tooltip`, `py-glyph.ts:70` and `ts-glyph.ts:135` give it
`title=`. Same button, same title bar, two hover behaviours.

### 2.11 Status feedback

Four unrelated mechanisms for "what is this glyph doing right now":

1. `createColorStateSetter` — tints container + title bar, states `idle`/`pending`/`orange`/`teal`
   (`query-glyph-states.ts:24-33`)
2. `updateStatus` in `prompt-glyph.ts:132-175` — `idle`/`running`/`success`/`error`, container
   background plus a status section, persisted to `localStorage` per glyph
3. `ui.statusLine()` (`glyph-ui.ts:146-170`) — auto-clearing green/red line, its own doc comment
   calls it "a weak design element", no in-tree consumer
4. `data-execution-state` CSS sweep — `running`/`completed`/`failed`
   (`web/css/glyph/execution-state.css`), driven by the `glyph_fired` WebSocket handler

Three of them define their own "running" and "error" colors. Only (4) is data-attribute driven
and therefore the only one visible to CSS.

### 2.12 Meta pill

`as-meta-pill` + `meta-popover as-meta-popover` (attestation `attestation-glyph.ts:110-122`,
triplet `triplet-glyph.ts:113-117`) and `glyph-meta-pill` + `meta-popover glyph-meta-popover`
(watcher queue stats, `websocket-handlers/watcher-queue-status.ts:170-196`) are two pills with
different geometry and placement. They collide, and the collision is handled by a guard:
`if (glyphEl.querySelector('.as-meta-pill')) return null;` (`watcher-queue-status.ts:172`) — the
watcher pill silently suppresses itself on any glyph that already has an attestation pill.

### 2.13 Sync/connectivity dataset wiring

`element.dataset.syncState` + `element.dataset.connectivityMode` is wired four times
(`ax-glyph.ts:274-285`, `semantic-glyph.ts:328-340`, `py-glyph.ts:179-187`,
`ts-glyph.ts:245-250`). The ax/se copies register `storeCleanup`; the py/ts copies **do not** —
those two subscriptions outlive the glyph.

## 3. Reinventions of primitives that already exist

- **`spawnTypeGlyph` (`type-glyph.ts:226-269`)** re-implements `spawnOnCanvas()` line for line —
  content-layer lookup, cursor-relative placement, registry lookup, render, append,
  `uiState.addCanvasGlyph` — and `type-glyph.ts` does not import `spawn-on-canvas` at all. Its
  siblings `attestation`, `triplet` and `sigma` do import the module, but call the *other* half,
  `spawnOnCanvasDragging`. The consequence is behavioural: double-clicking a
  type row drops a glyph at the cursor instantly, double-clicking an attestation, triplet or sigma
  row attaches one to the cursor until you click to place it. Same gesture, same result list, two
  placement models. `spawnOnCanvas` itself has no callers (§5.3).
- **`result-glyph` builds its own header twice**: `createResultGlyph` (`:293-345`, with
  `headerBtn`) and `buildResultTitleBar` (`:981-1022`, without). They have already drifted —
  the second has no color-mode toggle, and spells the copy icon `'\u2398'` where the first
  writes the literal `'⎘'`.
- **Result-spawn-below is written three times** — `prompt-glyph.ts:356-403`,
  `glyph-followup.ts`, `canvas-workspace-builder.ts`. Flagged in-code as
  `TODO [TS-5]` (`prompt-glyph.ts:353-355`) and still open.
- **Melded-attachment collection is written twice** — `prompt-glyph.ts:216-252` and
  `glyph-followup.ts`. Flagged as `TODO [TS-4]` (`prompt-glyph.ts:216-218`).
- **Proximity-reveal**: `thread-glyph.ts:50-62` hand-rolls a radius/`Math.hypot` reveal against
  a hardcoded `REVEAL_RADIUS = 80` while `GlyphProximity` (`packages/glyphs/proximity.ts`) is a
  tuned proximity engine with easing, thresholds and text fade. Two answers to "reveal on
  approach", one for the tray and one for the canvas, sharing nothing.
- **Typography tokens are bypassed**: `--font-mono` and `--font-size-sm: 11px` exist and are
  used 63 times in CSS — and once in all of `web/ts` (`glyph-module-loader.ts:155`). Glyph
  content instead writes the literal `monospace` 51 times and `'11px'` 43 times in
  `components/glyph/` alone.

## 4. Defects in glyph content

Missing primitives are a cost. These are breakage. None of them is covered by the 767 frontend
tests.

### 4.1 Deleting a glyph runs three different amounts of cleanup

- canvas delete — `removeGlyphElement` calls `runCleanup(el)`
  (`canvas-workspace-builder.ts:202-210`)
- error-glyph dismiss — calls `runCleanup(element)` (`error-glyph.ts:147-154`)
- note-glyph close — `element.remove()` + `uiState.removeCanvasGlyph()`, **no `runCleanup`**
  (`note-glyph.ts:162-167`)

Note-glyph is the file that registers the most teardown: `editorView.destroy()`, the autosave
cancel, and the ResizeObserver disconnect (`note-glyph.ts:316-322`). Its own close button is the
one path that runs none of them. Delete a note from the canvas and it tears down; close it with
its own ✕ and the ProseMirror view stays live.

### 4.2 py-glyph and ts-glyph register no cleanup at all

Neither file contains a single `storeCleanup` call. No `editor.destroy()`, no autosave cancel, no
unsubscribe. The delete path calls `runCleanup` into an empty list, so every removed py or ts
glyph leaves a live CodeMirror `EditorView` plus two subscriptions
(`syncStateManager.subscribe`, `connectivity.subscribe`, §2.13) whose callbacks hold the detached
element. Each removed glyph leaks its editor, its two subscriptions, and the DOM subtree they
hold.

### 4.3 Attestation fields reach `innerHTML` unescaped

`buildMetaLines` (`attestation-glyph.ts:32-59`) returns an array of HTML strings — it emits
`<span style="color: #00d4aa">signer: …</span>` by construction — and interpolates `actors`,
`source`, `source_version` and `id` raw. Three call sites assign the join to `innerHTML`
(`attestation-glyph.ts:117, 287, 494`). `escapeHtml` is not imported in that file. Those fields
are written by plugins and by `ts-glyph`'s in-page `qntx.attest()`, so an actor string containing
markup executes on hover.

The shared `renderTriple` primitive builds the same data with `textContent` and is safe
(`attestation-triple.ts:43-135`). The injection is in the hand-rolled meta pill — the same
component §2.12 lists as reinvented.

### 4.4 31 CSS custom properties are used and never defined

`css/` and `ts/` reference 41 `var(--…)` names that no stylesheet defines; 10 of those are
injected at runtime via `setProperty` (`--canvas-scale`, `--drawer-height`, `--note-*`,
`--glyph-border-opacity`, `--orbit-duration`). The remaining 31 silently fall back:

- A second, phantom token vocabulary parallel to `tokens.css`: `--text-color`, `--bg-color`,
  `--border-color`, `--error-color`, `--success-color`, `--color-primary`, `--color-text-secondary`,
  `--accent-blue/green/red` — beside the real `--text-primary`, `--border`, `--color-error`,
  `--color-success`, `--accent-color`.
- The entire `--ats-editor-*` family — 9 names, none defined, so `ats-code-block.css` renders
  wholly on its fallbacks.
- `--panel-border-color` is used 12 times with contradictory fallbacks: `#333`/`#444` in
  `type-definition-window.css`, `#e0e0e0`/`#d0d0d0` in `window.css:253,390,411`. Both paint on
  dark surfaces, so `.glyph-row` and `.db-stat-section` draw a near-white hairline.
- `result-glyph.ts:115` uses `var(--text-muted)` with no fallback at all. The declaration is
  invalid at computed-value time, so the stats line inherits its parent's color instead of a muted
  grey, dimmed only by `opacity: 0.6`.

### 4.5 Every result row carries a payload nothing reads

`dataset.attestation = JSON.stringify(...)` is written in five files (`ax-glyph.ts:312`,
`type-result-line.ts:84`, `semantic-glyph.ts:433`, `sigma-glyph.ts:634`,
`triplet-glyph.ts:487`) and read in none. `renderAttestationAttrs` renders inline PDB, GenBank and
FASTA payloads (`attestation-attrs.ts:459-505`), so an attestation carrying a structure file
serialises that file into an HTML attribute on every row that mentions it.

### 4.6 Streaming merge is quadratic

`updateAxGlyphResults` (`ax-glyph.ts:405-420`) handles each arriving attestation by parsing its
group back out of `dataset.tripletAttestations`, pushing, re-rendering the whole row, and
re-stringifying. A triplet group of n attestations costs O(n²) in JSON and in DOM rebuilds. The
type-attestation branch (`:376-400`) does the same.

### 4.7 Regex, which is banned

`prompt-glyph.ts:192` uses `/\{\{[^}]+\}\}/.test(template)` to decide whether a prompt has
variables, on the execute path.

## 5. Pending, in flight, and awaiting deletion

This section is work that started and stopped. The fix is to finish it or to delete it.

### 5.1 Declared migrations with no code path

- **Symbol palette → GlyphRun tray.** Root `CLAUDE.md:73` states it as in progress ("is being
  migrated to the GlyphRun tray — each palette action becomes a glyph with its own manifestation
  type"), `docs/vision/glyphs.md:180-190` describes the end state, and `default-glyphs.ts:12-50`
  carries a five-step plan naming it "the FIRST migration target". In the code the palette is
  static markup — twelve `<button class="palette-cell">` in `web/index.html:296-308`, wired by
  `symbol-palette.ts:63-120`, styled by `css/symbol-palette.css` (linked at `index.html:46`).
  Not a glyph, not in the tray, no bridge to one. The markup has also drifted from the generated
  source: `index.html:302` renders `is` as `==` where `sym.IS` is `=`. `initializeSymbolPalette`
  rewrites every cell's `textContent` from `@generated/sym.js` at startup, so the two disagree in
  the file and agree on screen.
- **`sym` → `glyph/sym` subpackage.** Stated in root `CLAUDE.md:71` and
  `docs/vision/glyphs.md:172`. `glyph/` contains `handlers`, `proto`, `storage`; `sym/` is still
  top-level. Nothing started.
- **`prompt-glyph` → GlyphUI SDK.** `prompt-glyph.ts:14` — "TODO: Migrate to GlyphUI SDK (like
  py-glyph and ts-glyph)". The only content-level migration TODO in the tree, and the target it
  names is itself partial: py and ts adopted `ui.glyph()` and nothing else (§1C).

### 5.2 Scheduled for deletion, blocked

- **`base-panel.ts`** is `@deprecated` and names both its blocker and its delete list
  (`base-panel.ts:1-8`): consumer `config-panel.ts`, and "also delete `base-panel-error.ts`,
  `css/panel-base.css`, `css/components/base-panel.css`". Both stylesheets are still linked
  (`index.html:24,43`). The catch: `base-panel-error.ts` holds `createLoadingState()`,
  `createErrorState()`, `createRichErrorState()` and `parseError()` — the exact empty/loading/error
  primitives §2.8 says glyph content lacks. The file scheduled for deletion contains the answer to
  an open gap. Deleting it as planned throws that away.
- **Legacy plugin HTML pipeline.** `plugin-provided-glyphs.ts:8-9` documents two rendering paths,
  "`module_url` → TypeScript SDK (preferred)" and "`content_url` only → server-rendered HTML via
  `innerHTML` (legacy)", dispatched at `:148-149`. `plugin-glyph.ts` is the legacy half and
  vocabulary D in §1: plugin glyph bodies are HTML strings from the server. Blocker: `qntx-atproto` is the only Go plugin declaring glyphs
  (`qntx-plugins/qntx-atproto/plugin.go:320-325`) and it declares `ContentPath` only.
- **Duplicated meld tests.** `packages/glyphs/meld/meldability.test.ts:3-6` says the web copy
  "may be removed once the package owns its own CI". The package now has its own CI
  (`packages/glyphs/README.md`, Publishing), but the copies have since diverged: the web copy
  carries 59 tests to the package's 58 — `'py can append to se (se leaf, right port)'` exists only
  in `web/ts/components/glyph/meld/meldability.test.ts`. Deleting the web copy today drops a test.
- **The `'stream'` symbol.** `result-glyph.ts:9` — "Replaces the former stream-glyph.ts and
  result-glyph.ts". The merge left `'stream'` in the registry (`glyph-registry.ts:61`, className
  `canvas-stream-glyph`), two CSS rules targeting it (`css/glyph/followup.css:24,87`), and a
  restore branch that quietly deletes orphaned empty stream glyphs from state
  (`canvas-workspace-builder.ts:318-323`). Self-cleaning with no end condition — nothing ever
  reports the population reaching zero.

### 5.3 Dead on arrival

- **`spawnOnCanvas()` has no callers.** `spawn-on-canvas.ts:36` — the module's own docstring says
  it exists to eliminate the repeated spawn pattern "across attestation, triplet, and sigma
  glyphs", and all three call `spawnOnCanvasDragging` instead (`attestation-glyph.ts:176`,
  `triplet-glyph.ts:363,461`, `sigma-glyph.ts:561`). The non-dragging half has been dead since it
  was written — and it is the half `spawnTypeGlyph` re-implemented by hand (§3), which is why type
  glyphs place instantly and the other three place on the cursor.
- **`manifestationType: 'ax' | 'cursor'`.** Declared in the union at `packages/glyphs/glyph.ts:21`,
  never set and never read anywhere in `web/` or `packages/`. `run.ts:187-196` branches on
  `'panel'` and `'canvas'` and treats everything else as `'window'`. `AXMT` in the package README's
  Deferred list is the open question for `'ax'`; `'cursor'` has no note at all.
- Exports with no consumer outside their own file: `buildResultTitleBar`,
  `subscribeStream`, `toggleColorMode` (`result-glyph.ts`), `spawnFollowUpResult`
  (`glyph-followup.ts`), `spawnTripletGlyph`, `spawnSigmaGlyph`, `QUERY_COLOR_STATES`. Each is
  reached only from inside its own file.

### 5.4 Half-built capabilities

- **Glyph conversion is capped at two destinations.** `conversions.ts:1-13` describes a general
  mechanism — same DOM element, tear down, repopulate as another type — and requires the target to
  export `setupXGlyph(element, glyph)`. Exactly two of the fourteen registered types do:
  `note-glyph.ts:66` and `prompt-glyph.ts:81`. So the module can only ever export what it exports
  today, `convertNoteToPrompt` and `convertResultToNote`. A third conversion,
  `convertErrorToPrompt`, lives privately in `error-glyph.ts:249` and never joined the module.
- **The registry is bypassed for the two most-used symbols.** `glyph-registry.ts:1-8` says it
  exists "eliminating parallel if/else chains in canvas-glyph.ts". `renderGlyph` runs two such
  chains ahead of the registry lookup: `'result'` (`canvas-workspace-builder.ts:269-316`) and
  `'stream'` (`:318-323`). `'result'` — the symbol every prompt and code execution spawns
  (`prompt-glyph.ts:372,384`, `canvas-workspace-builder.ts:499,511`) — is not in the registry at all,
  so it has no `className`, `title` or `spawnMenuOrder`, and `getGlyphTypeBySymbol('result')`
  returns undefined.

### 5.5 Content-format migrations still running

Glyph content is a persisted string, and five formats are mid-flight, each handled by a branch
with no cutover:

- result content — "new format has `.result`, old is raw ExecutionResult"
  (`canvas-workspace-builder.ts:289`), plus an error path whose stated cause is
  "Glyph metadata saved without execution result (migration bug)" (`:281`)
- semantic query content — JSON vs "legacy string", caught in two places
  (`semantic-glyph.ts:52,62`). `ax` stores its query as a raw string and `se` stores JSON: the same
  title-bar input, two content encodings.
- sigma aggregates — `{frequencies, count}` vs "legacy `{values, count}`"
  (`sigma-glyph.ts:113,341`)
- compositions — old format without an `edges` array is detected and removed
  (`canvas-workspace-builder.ts:799-801`)
- prompt status — `localStorage['prompt-status-{glyphId}']` (`prompt-glyph.ts:43-66`) holds glyph
  content state outside canvas state entirely. Not marked legacy, not synced, not migrated.

### 5.6 Branches still open

`#765` "Focus manifestation: thread layout + DAG subgraph" is not in main —
`packages/glyphs/manifestations/` holds canvas-placed, canvas-window, canvas, cursor, morphology,
panel, render-content, stash, title-bar-controls, window, and no focus.

Three other glyph PRs are open and untouched since 2026-05-25 while the features they name are in
main with commits since: `#442` "canvas ↔ window manifestation morphing"
(`packages/glyphs/manifestations/canvas-window.ts`), `#461` "DONTMERGE: Subcanvas glyph"
(`web/ts/components/glyph/subcanvas-glyph.ts`), `#481` "Harden glyph system: fix bugs, add tests,
eliminate drift sources".

## 6. Where the leverage is

§4 comes first: it is breakage, and 4.1–4.3 are each a few lines.

1. **A destroy primitive** (§4.1, §4.2) — one `destroyGlyph(element, glyphId)` that runs cleanup,
   removes from state, and removes from the DOM, called by all three delete paths. Fixes the
   note-glyph close button and gives py/ts somewhere to enrol. The axiom governs a glyph's birth
   and its morphs; nothing governs its death, which is why death has three implementations.
2. **`escapeHtml` in `buildMetaLines`** (§4.3) — or build the meta popover with `textContent` the
   way `renderTriple` already does.
3. **Define the 31 names, or delete their call sites** (§4.4) — the phantom vocabulary is a
   silent failure mode: an undefined custom property renders, and renders wrong.
4. **Result row** (§2.1) — one primitive, four call sites, the unit the query glyphs are made of.
   The four rows carry different shapes — one attestation, a type group, a triplet group, a sigma
   report — so this needs a slot API.
5. **Attestation-family shell** (§2.5) — collapses four ~40-line preambles and re-converges the
   drifted background.
6. **Code-editor glyph** (§2.3, editor half only) — makes a third language a registry entry
   instead of a file.
7. **Identity palette** (§2.7) — `glyph.color` already exists on the Glyph model and persists per
   instance. A registry palette is a second home for the same fact. One of them has to win.
8. **Action button + symbol set** (§2.6) — promote `headerBtn` out of its closure and give the
   action icons one home. That home is `glyph/sym`, §5.1's unstarted migration. Today's `sym` is
   generated from Go and shared with the CLI and the docs; run/expand/copy/close are browser
   chrome.
9. **Query-glyph shell** (§2.4), **status feedback** (§2.11), **empty/loading/error** (§2.8).

`GlyphUI` (§1C) is the home for most of the extractions: it is the documented surface for building
a glyph and it is already what plugins get. Its form primitives were scoped for plugins, and the
package README's `GLYUI` entry defers extraction until a second consumer appears. No equivalent
exists for the glyphs in `web/ts/`.
