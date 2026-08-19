# Glyph Content Coherence

Glyph chrome is unified: `canvasPlaced()` owns the container, `.glyph-title-bar` was averaged from
six implementations, `wireExpandToWindow()` replaced the copy-pasted expand handler. Everything
below the title bar has had no such pass. 105 findings, each with the line that shows it.

## Vocabularies

- [ ] Four vocabularies build glyph content, and no glyph uses more than one.
      CSS classes + `innerHTML` (`window.css:241-355`), inline `el()` styles, GlyphUI primitives, server HTML.
- [ ] The GlyphUI form primitives have no consumer under `web/ts/`.
      `ui.input`/`ui.button`/`ui.statusLine` (`glyph-ui.ts:120-170`) — only `qntx-plugins/ix-json/web/glyph-module.ts:40-58`.
- [ ] Two content-area classes compete for the same slot.
      `.glyph-content-area` in nine glyphs, `.glyph-content` in four, neither in `prompt-glyph.ts:320` or `note-glyph.ts:174`.
- [ ] Plugin glyph bodies are HTML strings from a Go handler, re-executing their own `<script>` tags.
      `plugin-glyph.ts:137-160`, the legacy half of the `content_url` / `module_url` split.

## Missing primitives

- [ ] Three result rows are still built by hand beside the shared one.
      `type-result-line.ts:73`, `triplet-glyph.ts:474`, `sigma-glyph.ts:620` vs `attestation-result-row.ts:35`.
- [ ] Each hand-built row spawns its own glyph type, which the shared row hardcodes.
      `attestation-result-row.ts:58-61` calls `spawnAttestationGlyph` unconditionally.
- [ ] The dot-separated fact list inside those rows is written three times.
      `type-result-line.ts:117-153`, `sigma-glyph.ts:657-685`, `result-glyph.ts:107-119`.
- [ ] The key/value block is written four times inside one function.
      `attestation-attrs.ts:459-516` — same `marginBottom: 4px`, same `10px` key label, four times.
- [ ] Two key/value layouts exist, one named and one not.
      `.glyph-row` is a two-column `space-between` (`window.css:249-262`, 17 uses); the attestation block stacks.
- [ ] The py and ts editor halves are duplicated whole.
      Height calc, CodeMirror boot, autosave, `(element as any).editor`, error fallback — `py-glyph.ts`, `ts-glyph.ts`.
- [ ] The query-glyph shell is duplicated across ax and se.
      Title-bar input, 500 ms debounce, `watcher_upsert` lifecycle, resize observer — `ax-glyph.ts:96-290`, `semantic-glyph.ts:208-345`.
- [ ] The attestation-family shell is hand-built four times.
      `attestation-glyph.ts:100-160`, `triplet-glyph.ts:386-446`, `type-glyph.ts:165-215`, `sigma-glyph.ts:498-545`.
- [ ] Its container background has drifted between the four.
      Three use `rgba(25, 25, 30, 0.95)`, `triplet-glyph.ts` uses `rgba(30, 35, 42, 0.95)`.
- [ ] Copy is a different character in two glyphs.
      `⎘` U+2398 at `result-glyph.ts:335`, `📋` emoji at `error-glyph.ts:111`.
- [ ] Dismiss is a different character in two places.
      `✕` U+2715 at `error-glyph.ts:146`, `×` U+00D7 at `note-glyph.ts:123` and `title-bar-controls.ts:38`.
- [ ] The same character is a literal in one file and an escape in another.
      Run: `'▶'` at `py-glyph.ts:68` vs `'▶'` at `ts-glyph.ts:133`. Copy: `result-glyph.ts:335` vs `:1004`.
- [ ] `SO` is imported from generated sym in one file and hardcoded in two.
      `prompt-glyph.ts` imports it; `error-glyph.ts:124,135` and `canvas/action-bar.ts:66` write `⟶`.
- [ ] "Expand to window" is retyped as a `title` string in six files.
      No shared action-button factory exists to carry it.
- [ ] The action-button factory exists, inside a function body, unexported.
      `result-glyph.ts:314-318` — three lines, invisible to every other glyph.
- [ ] Four private accent palettes carry the same accent/value/dim triple.
      `sigma-glyph.ts:21-25`, `triplet-glyph.ts:24-28`, `type-glyph.ts:22-24`, `attestation-attrs.ts:18-21`.
- [ ] A fifth palette is eight literal reds.
      `thread-glyph.ts:31-39`.
- [ ] The registry entry carries no color, so py and ts pass theirs as literals.
      `glyph-registry.ts:28-46`; `#2a5578`/`#FFD43B` and `#5c3d1a`/`#f0c878` go to `ui.glyph()`.
- [ ] The empty-state helper is scoped to query glyphs and keyed by a class-name string.
      `query-glyph-states.ts:37-46`; callers `querySelector` it back by that string (`ax-glyph.ts:349`).
- [ ] Empty state elsewhere is ad-hoc text in four shapes.
      `result-glyph.ts:263`, `chart-glyph.ts:178`, `doc-glyph.ts:72`, `db-glyph.ts:252,338`.
- [ ] Loading state is three shapes.
      `ax-glyph.ts:134-141` inline, `chart-glyph.ts:134` `.glyph-loading`, `plugin-glyph.ts:94` string.
- [ ] The loading/error primitives exist and are used by panels only.
      `base-panel-error.ts:57-170`; `UI_TEXT.NO_DATA`/`LOADING` (`config.ts:6-14`) used by neither.
- [ ] The mandated error surface is absent from glyph content.
      `Button` is used at 8 sites in 3 files; `components/glyph/` has 29 raw `<button>` creations.
- [ ] A failed action inside a glyph surfaces in five different presentations.
      `py-glyph.ts:112`, `py-glyph.ts:114-121`, `prompt-glyph.ts:132-175`, `query-glyph-states.ts:52-96`, `error-glyph.ts`.
- [ ] Two hover systems run inside glyph content.
      `has-tooltip` + `data-tooltip` at 37 sites, native `title=` at 15 in `components/glyph/`.
- [ ] The same run button has different hover behaviour per file.
      `prompt-glyph.ts:179` uses `has-tooltip`; `py-glyph.ts:70` and `ts-glyph.ts:135` use `title=`.
- [ ] Four unrelated mechanisms answer "what is this glyph doing".
      `query-glyph-states.ts:24-33`, `prompt-glyph.ts:132-175`, `glyph-ui.ts:146-170`, `execution-state.css`.
- [ ] Three of the four define their own running and error colors.
      Only `data-execution-state` is attribute-driven, so only it is visible to CSS.
- [ ] Two meta-pill systems collide, and one suppresses itself.
      `as-meta-pill` vs `glyph-meta-pill`; guard at `watcher-queue-status.ts:172`.
- [ ] The sync/connectivity dataset wiring is repeated four times.
      `ax-glyph.ts:274-285`, `semantic-glyph.ts:328-340`, `py-glyph.ts:179-187`, `ts-glyph.ts:245-250`.

## Reinventions

- [ ] `spawnTypeGlyph` re-implements `spawnOnCanvas` line for line and never imports it.
      `type-glyph.ts:226-269` — content layer, placement, registry lookup, append, state.
- [ ] Type rows place instantly while attestation, triplet and sigma rows place on the cursor.
      Same gesture, same list; the other three call `spawnOnCanvasDragging`.
- [ ] `result-glyph` builds its own header twice, and the two have drifted.
      `:293-345` with `headerBtn`, `:981-1022` without the color toggle.
- [ ] Result-spawn-below is written three times, flagged in code, still open.
      `prompt-glyph.ts:356-403`, `glyph-followup.ts`, `canvas-workspace-builder.ts`; `TODO [TS-5]` at `:353`.
- [ ] Melded-attachment collection is written twice, flagged in code, still open.
      `prompt-glyph.ts:216-252` and `glyph-followup.ts`; `TODO [TS-4]` at `:216`.
- [ ] Proximity-reveal is hand-rolled on canvas against a tuned engine for the tray.
      `thread-glyph.ts:50-62` with `REVEAL_RADIUS = 80` vs `packages/glyphs/proximity.ts`.
- [ ] The typography tokens are bypassed in glyph content.
      `--font-mono` used 68× in CSS, 1× in `web/ts`; literal `monospace` 52× and `'11px'` 43× in `components/glyph/`.

## Defects

None of these is covered by the frontend test suite.

- [ ] Deleting a glyph runs three different amounts of cleanup.
      `canvas-workspace-builder.ts:202-210` and `error-glyph.ts:147-154` call `runCleanup`; `note-glyph.ts:162-167` does not.
- [ ] Note-glyph's own close button skips the three teardowns note-glyph registers.
      `note-glyph.ts:316-322` registers `editorView.destroy()`, autosave cancel, observer disconnect.
- [ ] py-glyph and ts-glyph contain no `storeCleanup` call at all.
      `runCleanup` runs an empty list; the `EditorView` and two subscriptions outlive the element.
- [ ] Attestation fields reach `innerHTML` unescaped.
      `buildMetaLines` (`attestation-glyph.ts:32-59`) → `innerHTML` at `:117, :287, :494`; no `escapeHtml` import.
- [ ] Those fields are writable by plugins and by in-page `qntx.attest()`.
      `actors`, `source`, `source_version`, `id` are interpolated raw into an HTML string.
- [ ] 36 CSS custom properties are used and never defined.
      42 referenced, 6 injected at runtime via `setProperty`.
- [ ] A second token vocabulary sits beside `tokens.css`, none of it defined.
      `--text-color`, `--bg-color`, `--border-color`, `--error-color`, `--success-color`, `--color-primary`.
- [ ] The whole `--ats-editor-*` family is undefined, so that stylesheet renders on fallbacks.
      Nine names, zero definitions.
- [ ] One undefined variable has fallbacks that disagree about the theme.
      `--panel-border-color`: `#333`/`#444` in `type-definition-window.css`, `#e0e0e0`/`#d0d0d0` in `window.css:253,390,411`.
- [ ] One `var()` has no fallback, so the declaration is invalid and the color inherits.
      `result-glyph.ts:115` uses `var(--text-muted)`; the stats line is not muted, only `opacity: 0.6`.
- [ ] `dataset.attestation` is written in four files and read in none.
      `attestation-result-row.ts:54`, `type-result-line.ts:84`, `sigma-glyph.ts:634`, `triplet-glyph.ts:487`.
- [ ] The shared row states a reason for it four lines above a handler that ignores it.
      `attestation-result-row.ts:53` vs the dblclick closure at `:58`.
- [ ] Attestations carrying inline PDB, GenBank or FASTA serialise those files into that attribute.
      `attestation-attrs.ts:459-505` renders them, so they are in the attestation the row stringifies.
- [ ] The AX streaming merge is quadratic.
      `ax-glyph.ts:405-420` re-parses, re-renders and re-stringifies the group per arrival; `:376-400` the same.
- [ ] Regex is used on the prompt execute path, and regex is banned.
      `prompt-glyph.ts:192` — `/\{\{[^}]+\}\}/.test(template)`.

## Pending, in flight, dead

- [ ] The symbol palette → GlyphRun migration is declared in three places and has no code path.
      `CLAUDE.md:73`, `docs/vision/glyphs.md:180-190`, `default-glyphs.ts:12-50`.
- [ ] The palette is still static markup with its own stylesheet.
      Twelve buttons at `index.html:296-308`, wired by `symbol-palette.ts:63-120`, styled by `symbol-palette.css`.
- [ ] The palette markup has drifted from generated sym.
      `index.html:302` renders `is` as `==` where `sym.IS` is `=`; init rewrites every cell at startup.
- [ ] The `sym` → `glyph/sym` migration is declared twice and has not started.
      `CLAUDE.md:71`, `docs/vision/glyphs.md:172`; `glyph/` holds handlers, proto, storage.
- [ ] The one content-level migration TODO names a target that is itself partial.
      `prompt-glyph.ts:14`; py and ts adopted `ui.glyph()` and nothing else.
- [ ] `base-panel.ts` is deprecated and names its own delete list, still blocked.
      `base-panel.ts:1-8`; both stylesheets still linked at `index.html:24,43`.
- [ ] That delete list includes the file holding the loading/error primitives glyph content lacks.
      `base-panel-error.ts` — `createLoadingState`, `createErrorState`, `parseError`.
- [ ] The legacy plugin HTML path is blocked by the one Go plugin declaring glyphs.
      `qntx-plugins/qntx-atproto/plugin.go:320-325` declares `ContentPath` only.
- [ ] The duplicated meld tests have diverged, so deleting the copy drops a test.
      Web copy 59 tests, package copy 58; `'py can append to se'` exists only in the web copy.
- [ ] The `'stream'` symbol is a tombstone with a self-cleaning branch and no end condition.
      `glyph-registry.ts:61`, `css/glyph/followup.css:24,87`, `canvas-workspace-builder.ts:318-323`.
- [ ] `spawnOnCanvas()` has no callers.
      `spawn-on-canvas.ts:36`; all three glyphs its docstring names call `spawnOnCanvasDragging`.
- [ ] Two `manifestationType` values are declared, never set and never read.
      `packages/glyphs/glyph.ts:21` — `'ax'` and `'cursor'`; `run.ts:187-196` branches on two others.
- [ ] Seven exports are reached only from inside their own file.
      `buildResultTitleBar`, `subscribeStream`, `toggleColorMode`, `spawnFollowUpResult`, `spawnTripletGlyph`, `spawnSigmaGlyph`, `QUERY_COLOR_STATES`.
- [ ] Glyph conversion is structurally capped at two of fourteen types.
      `conversions.ts:1-13` needs `setupXGlyph`; only `note-glyph.ts:66` and `prompt-glyph.ts:81` export it.
- [ ] A third conversion lives outside the conversion module.
      `convertErrorToPrompt` is private to `error-glyph.ts:249`.
- [ ] The registry is bypassed by two if/else chains it says it exists to eliminate.
      `canvas-workspace-builder.ts:269-316` for `'result'`, `:318-323` for `'stream'`.
- [ ] `'result'`, the symbol every prompt spawns, is not in the registry.
      `getGlyphTypeBySymbol('result')` returns undefined; it has no className, title or spawn order.
- [ ] Result content has two formats and a branch, plus an error path naming a migration bug.
      `canvas-workspace-builder.ts:289` and `:281`.
- [ ] ax and se store the same title-bar input in two encodings.
      `ax` writes a raw string; `se` writes JSON with a legacy-string fallback at `semantic-glyph.ts:52,62`.
- [ ] Sigma aggregates have two formats and a branch.
      `{frequencies, count}` vs legacy `{values, count}` — `sigma-glyph.ts:113,341`.
- [ ] Old-format compositions are detected and deleted on load.
      `canvas-workspace-builder.ts:799-801`, missing `edges` array.
- [ ] Prompt status lives in `localStorage`, outside canvas state, unsynced and unmigrated.
      `prompt-glyph.ts:43-66` — `prompt-status-{glyphId}`.
- [ ] One open PR's feature is genuinely unlanded.
      `#765` focus manifestation; `packages/glyphs/manifestations/` has no focus.
- [ ] Three open PRs sit untouched since 2026-05-25 while what they built is in main.
      `#442` canvas↔window morph, `#461` subcanvas, `#481` harden glyph system.

## Landed on top

- [ ] A new top-level bar was added beside the palette, in the palette's shape.
      `namespaces-bar.ts` — `innerHTML` (`namespaces-view.ts:47`), own stylesheet (`index.html:31`), not a glyph.
- [ ] The newest UI styles its buttons and inputs inline against four existing vocabularies.
      `ceremony.ts:39-70`; `web/ts` now has 39 buttons with no class, vs `titlebar-btn` 11, `panel-btn` 10, `glyph-btn` 1.
- [ ] A fifth family of small coloured label landed; CSS now defines 31.
      `handlers-pill`, `-arg`, `-watch`, `-schedule`, `-handler` (`css/handlers-panel.css:140-182`).
- [ ] `escapeHtml` does not escape quotes.
      `html-utils.ts:21-24` — `textContent`→`innerHTML` escapes `&`, `<`, `>`; `escapeHtml('a"b')` returns `a"b`.
- [ ] Fifteen sites interpolate it inside a double-quoted attribute.
      `plugin-panel.ts:428,429,589,627-632,634,639,645`, `embeddings-glyph.ts:519,523,627`, `namespaces-view.ts:37`.
- [ ] Plugin name, version, config description, `value=` and `pattern=` are among them.
      All plugin-supplied; a value containing `"` closes the attribute.
- [ ] `copyable` restores a stale snapshot over whatever the element says a second later.
      `copyable.ts:35-40` — snapshot at click, `copied`, unconditional restore at 1000 ms.
- [ ] It is applied to the sign-in status line, which the flow rewrites at fourteen points.
      `auth-glyph.ts:161`; rewrites at `:219-570`.
- [ ] Two copy mechanisms now coexist with two timings.
      `copyable` (1000 ms, text swap) beside `result-glyph.ts:335` (1500 ms, icon swap) and `error-glyph.ts:111`.
- [ ] The generated identity model has no importer.
      `generated/proto/plugin/grpc/protocol/user.ts` — `User`, `Key`, `Account`, `Binding`, `AccessLevel`, 155 lines.
- [ ] `AccessLevel.SUPER` appears in `web/ts` only in that unused file and three comments.
      The frontend has the contract and does not read it.
- [ ] Namespaces have a UI and no generated contract.
      `namespaces-view.ts:3-13` hand-declares `Namespace` and `NamespaceOwner`; no proto defines one.
- [ ] Nothing in CI runs the lint that carries the bans.
      `make lint` exists and `make test` depends on it (`Makefile:190-196`); `ts.yml` runs typecheck and tests only.
- [ ] The lint config does not cover the two rules found broken.
      The regex ban and interpolated `innerHTML` are both `no-restricted-syntax` selectors.

## Tool output

`knip` over `web/`, entry `ts/main.ts` plus build scripts and tests, `ts/generated/**` ignored. Exit 1.

- [ ] Six files are unreachable from any entry point.
      `accessibility.ts`, `api/canvas-export.ts`, `embedding-store.ts`, `local-semantic-search.ts`, `filetree/navigator.ts`, `pulse/ats-node-view.ts`.
- [ ] Two of them are an island a single-level grep calls live.
      `local-semantic-search.ts` imports `embedding-store.ts`; nothing imports `local-semantic-search.ts`.
- [ ] The dead canvas export has a live server-side counterpart.
      `exportCanvasDOM(workspace)` unused; the Export button (`canvas-expanded.ts:114-128`) calls `exportCanvasStatic`.
- [ ] Four more exports in `base-panel-error.ts` have no consumer outside the file.
      `createErrorState`, `createLoadingState`, `parseError`, `showRichError`.
- [ ] The new shared row exports a tint constant nobody imports.
      `RESULT_ROW_TINT` at `attestation-result-row.ts:13`.
- [ ] Six symbols are re-exported through a barrel nothing consumes.
      `attestation-glyph.ts:26` re-exports from `attestation-attrs`.
- [ ] No test file is type-checked.
      `tsconfig.json` excludes `**/*.test.ts`; `tsc --listFiles` puts 0 test files in the program.
- [ ] A test imports `vitest`, which is not installed, and passes.
      `type-definition-window.test.ts:1`; no `node_modules/vitest`, absent from `package.json`.
- [ ] A test imports a module that does not exist, and passes.
      `run.dom.test.ts:11` imports `./glyph.ts`; the type-only import is erased before it can fail.
- [ ] One dependency resolves only transitively.
      `markdown-it` at `prose/note-markdown.ts:9`, absent from `package.json`, reached via `prosemirror-markdown`.
- [ ] One module exports the same value twice.
      `state/ui.ts` exports `uiState` named and as default.
