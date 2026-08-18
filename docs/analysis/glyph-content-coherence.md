# Glyph Content Coherence

Glyph *chrome* has been unified. `canvasPlaced()` owns container, layout, drag, resize.
`.glyph-title-bar` was "averaged from six prior implementations" (`web/css/glyph/title-bar.css:1-9`).
`wireExpandToWindow()` replaced "the copy-pasted expand-button click handler that existed in
every glyph file" (`packages/glyphs/expand-to-window.ts:1-7`). `spawnOnCanvas()` replaced the
repeated spawn pattern (`web/ts/components/glyph/spawn-on-canvas.ts:1-8`).

Glyph *content* — everything below the title bar — has had no such pass. This is an inventory
of what is duplicated there, what primitive is missing, and what already exists but is unused.

## 1. Three content vocabularies coexist

**A. CSS-class vocabulary.** `.glyph-content`, `.glyph-row`, `.glyph-label`, `.glyph-value`,
`.glyph-section`, `.glyph-section-title`, `.glyph-loading` (`web/css/window.css:241-355`),
built via `innerHTML` templates. Used by the tray/window glyphs: `db-glyph.ts`,
`embeddings-glyph.ts`, `default-glyphs.ts`, `llm-provider-glyph.ts`.

**B. Inline `el()` styles.** Every canvas glyph: `ax`, `se`, `py`, `ts`, `prompt`, `note`,
`result`, `attestation`, `triplet`, `type`, `sigma`, `error`. No shared class names below the
title bar; padding, font, and color are re-declared per call site.

**C. GlyphUI form primitives.** `ui.input()`, `ui.button()`, `ui.statusLine()` →
`.glyph-form-group`, `.glyph-input`, `.glyph-btn` (`web/ts/components/glyph/glyph-ui.ts:120-170`,
`web/css/window.css:292-340`). **Zero in-tree consumers.** The only caller in the repo is the
out-of-tree plugin `qntx-plugins/ix-json/web/glyph-module.ts:45-58`. `py-glyph` and `ts-glyph`
use `ui.glyph()` and then hand-roll everything inside it.

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
`10px` / `--text-secondary` / `marginBottom: 1px`. `.glyph-row` + `.glyph-label` +
`.glyph-value` already exist in CSS for exactly this and are not used here.

### 2.3 Code-editor glyph

`py-glyph.ts` and `ts-glyph.ts` are near-identical: the same height calculation
(`lineHeight 24`, `titleBarH 36`, `min 120`, `max 600`), the same `▶` titlebar button, the same
CodeMirror dynamic-import boot with `defaultKeymap`/`oneDark`/`lineWrapping`, the same
`createAutoSave` wiring, the same `(element as any).editor` stash, the same "Error loading
editor" fallback, the same sync/connectivity subscriptions. The delta is the language extension
and where execution goes (`/execute` vs `AsyncFunction`). A third language means a third copy.

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

- `appendEmptyState()` exists but is scoped to query glyphs and takes a per-glyph class name
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
  `uiState.addCanvasGlyph` — while its siblings `attestation`, `triplet` and `sigma` call the
  shared helper. `type-glyph.ts` does not import `spawn-on-canvas`.
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

## 4. Rule breaches found in glyph content

- **Regex is banned** (root `CLAUDE.md`) — `prompt-glyph.ts:192` uses
  `/\{\{[^}]+\}\}/.test(template)` to decide whether a prompt has variables. This is on the main
  execute path, not a dev tool.
- **Leaked subscriptions** — `py-glyph.ts:179,184` and `ts-glyph.ts:245,248`, see §2.13.

## 5. Where the leverage is

Ordered by copies removed per primitive introduced:

1. **Result row** (§2.1) — one primitive, four call sites, and it is the unit the query glyphs
   are made of. Everything else in an AX glyph is chrome.
2. **Action button + symbol set** (§2.6) — promote `headerBtn` out of its closure, put run /
   expand / copy / close / dismiss in `sym` next to the domain symbols, one hover behaviour.
   Touches every glyph, changes no layout.
3. **Attestation-family shell** (§2.5) — collapses four ~40-line preambles and re-converges the
   drifted background.
4. **Code-editor glyph** (§2.3) — makes a third language a registry entry instead of a file.
5. **Identity palette on the registry** (§2.7) — gives §2.1 and §2.5 the tint they currently
   hardcode, and makes `--font-mono` / `--font-size-sm` reachable from content (§3).
6. **Query-glyph shell** (§2.4), **status feedback** (§2.11), **empty/loading/error** (§2.8).

`GlyphUI` (§1C) is the natural home for most of these: it is the documented surface for building
a glyph, it is already what plugins get, and the fact that no in-tree glyph calls
`ui.input`/`ui.button`/`ui.statusLine` is the clearest single signal that content primitives are
missing rather than merely unused.
