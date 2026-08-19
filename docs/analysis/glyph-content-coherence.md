# Glyph Content Coherence

Glyph chrome is unified: `canvasPlaced()` owns the container, `.glyph-title-bar` was averaged from
six implementations, `wireExpandToWindow()` replaced the copy-pasted expand handler. Everything
below the title bar has had no such pass. Each box is the change; the line under it is what shows
the need and what makes it more than a substitution.

## Vocabularies

- [ ] Name the one vocabulary glyph content uses, in `web/CLAUDE.md`.
      Four are live: CSS classes + `innerHTML`, inline `el()` styles, GlyphUI primitives, server HTML.
- [ ] Adopt `ui.input`/`ui.button`/`ui.statusLine` in one glyph, or delete them.
      No consumer under `web/ts/`. The only one is the `ix-json` glyph module.
- [ ] Collapse `.glyph-content` into `.glyph-content-area`.
      Nine glyphs use one, four the other; `setupPromptGlyph` and `setupNoteGlyph` use neither.
- [ ] Delete the `content_url` rendering path.
      `fetchPluginContent` injects server HTML and re-runs its `<script>` tags. Blocked below.

## Missing primitives

- [ ] Add a `spawn` option to `attestationResultRow`, then route the three hand-built rows through it.
      `attestationResultRow` hardcodes `spawnAttestationGlyph`, and `renderTypeResultLine`, `renderTripletResultLine` and `renderSigmaResultLine` each open a different glyph.
- [ ] Move the dot-separated fact list into `attestation-result-row.ts`.
      Written three times: `renderTypeResultLine`, `renderSigmaResultLine`, `renderStatsLine`.
- [ ] Extract the key/value block out of `renderAttestationAttrs`.
      `renderAttestationAttrs` repeats it four times inside the one function.
- [ ] Give the stacked key/value layout a class, or make the attestation block use `.glyph-row`.
      `.glyph-row` is two-column `space-between` and has 17 uses; `renderAttestationAttrs` stacks the key above the value.
- [ ] Extract the editor half of py and ts; leave execution in each glyph.
      `createPyGlyph` and `createTsGlyph` share height calc, CodeMirror boot, `createAutoSave`, the `editor` stash and the error fallback.
- [ ] Extract the query-glyph shell.
      `createAxGlyph` and `createSemanticGlyph` repeat the input, the 500 ms debounce, the `watcher_upsert` lifecycle and `setupGlyphResizeObserver`.
- [ ] Extract the attestation-family shell and pick one container background.
      `createAttestationGlyph`, `createTripletGlyph`, `createTypeGlyph`, `createSigmaGlyph`; `TRIPLET_BG` differs from the other three.
- [ ] Pick one copy character and one dismiss character.
      Copy: `⎘` in `createResultGlyph`, `📋` in `createErrorGlyph`. Dismiss: `✕` in `createErrorGlyph`, `×` in `setupNoteGlyph` and `addWindowControls`.
- [ ] Write symbols as literals throughout, or as escapes throughout.
      Run is the literal in `createPyGlyph` and the escape in `createTsGlyph`; copy differs the same way between `createResultGlyph` and `buildResultTitleBar`.
- [ ] Import `SO` from generated sym in `createErrorGlyph` and the canvas action bar.
      `setupPromptGlyph` imports it; both convert buttons write the `⟶` literal.
- [ ] Export `headerBtn` and build the expand button through it.
      `headerBtn` is three lines inside `createResultGlyph`; "Expand to window" is retyped as a `title` in six files.
- [ ] Put the accent palettes on the registry entry or in `tokens.css`.
      `AMBER`, `TRIPLET`, `TYPE_COLOR` and `AZURE` carry the same triple, `THREAD_COLORS` a fifth; `GlyphTypeEntry` has no color field.
- [ ] Give `appendEmptyState` a fixed class and stop threading the name through callers.
      `appendEmptyState` takes the class name as an argument, and callers `querySelector` it back by that same string.
- [ ] Route the four ad-hoc empty states and three loading states through one helper.
      Empty in `renderOutput`, `ChartGlyph`, `createDocGlyph`, `renderDbStats`. Loading in `createAxGlyph`, `ChartGlyph`, `createPluginGlyph`.
- [ ] Move `createLoadingState`, `createErrorState` and `parseError` out of `base-panel-error.ts`.
      That file is on `BasePanel`'s own delete list, and those three are what glyph content lacks.
- [ ] Use `Button` for actions inside glyphs, or drop the rule from `web/CLAUDE.md`.
      `components/glyph/` has 29 raw `<button>` creations; `Button` is used at 8 sites, all outside it except `morphFullscreenToCanvasPlaced`.
- [ ] Pick one presentation for a failed action inside a glyph.
      Five exist: `ui.log.error`, a spawned result, `updateStatus`, `showQueryError`, `createErrorGlyph`.
- [ ] Pick one hover system.
      `has-tooltip` at 37 sites, `title=` at 15; the run button uses `has-tooltip` in `setupPromptGlyph` and `title=` in `createPyGlyph`.
- [ ] Pick one status mechanism and drive it from a data attribute.
      `createColorStateSetter`, `updateStatus`, `ui.statusLine`, `data-execution-state`; three define their own running and error colors.
- [ ] Merge `as-meta-pill` and `glyph-meta-pill`, then delete the suppression guard.
      `ensureMetaPill` returns null on any glyph that already carries an `as-meta-pill`.
- [ ] Extract the sync/connectivity dataset wiring into one call.
      `syncStateManager.subscribe` and `connectivity.subscribe` are wired the same way in four glyph factories.

## Reinventions

- [ ] Point `spawnTypeGlyph` at the shared spawn, or record that type glyphs place instantly on purpose.
      `spawnTypeGlyph` re-implements `spawnOnCanvas`; `spawnAttestationGlyph`, `spawnTripletGlyph` and `spawnSigmaGlyph` call `spawnOnCanvasDragging`.
- [ ] Build the result header once.
      `createResultGlyph` and `buildResultTitleBar` have drifted; the second lost the color-mode toggle.
- [ ] Extract `spawnResultBelow`.
      `spawnResponseBelow`, `spawnFollowUpResult` and the canvas workspace builder; `TODO [TS-5]` is still open.
- [ ] Extract `collectMeldedAttachments`.
      `setupPromptGlyph` and `createFollowUpZone`; `TODO [TS-4]` is still open.
- [ ] Use `GlyphProximity` for the thread reveal, or record why canvas needs its own.
      `wireProximityReveal` hand-rolls a `Math.hypot` check against `REVEAL_RADIUS`.
- [ ] Replace literal `monospace` and `'11px'` with the tokens.
      52 and 43 occurrences in `components/glyph/`; `--font-mono` is used 68× in CSS and once in all of `web/ts`.

## Defects

None of these is covered by the frontend test suite.

- [ ] Add `destroyGlyph(element, glyphId)` and call it from all three delete paths.
      `removeGlyphElement` and `createErrorGlyph`'s dismiss call `runCleanup`; `setupNoteGlyph`'s close button does not, so its ✕ leaves the ProseMirror view, autosave and observer live.
- [ ] Register cleanup in py-glyph and ts-glyph.
      Neither `createPyGlyph` nor `createTsGlyph` calls `storeCleanup`, so `runCleanup` runs an empty list and the `EditorView` and both subscriptions outlive the element.
- [ ] Build the meta popover with `textContent`.
      `buildMetaLines` returns HTML strings with `actors`, `source` and `id` interpolated raw, and three sites assign the join to `innerHTML`. `renderTriple` builds the same data with `textContent`.
- [ ] Define the 36 undefined custom properties, or delete their call sites.
      42 referenced, 6 injected by `setProperty`; the rest includes `--text-color`, `--bg-color`, `--border-color`, `--error-color` beside the real names in `tokens.css`.
- [ ] Define `--ats-editor-*`, or inline the fallbacks.
      Nine names, zero definitions, so `ats-code-block.css` renders wholly on its fallbacks.
- [ ] Define `--panel-border-color`.
      Its fallbacks disagree about the theme: `#333` in `type-definition-window.css`, `#e0e0e0` in `window.css`, both painting on dark surfaces.
- [ ] Define `--text-muted`, or give `renderStatsLine` a fallback.
      With neither, the declaration is invalid at computed-value time and the stats line inherits its parent's color.
- [ ] Delete the `dataset.attestation` writes.
      Four writers, no reader; and `renderAttestationAttrs` renders inline PDB and FASTA, so those files serialise into the attribute.
- [ ] Hold the triplet group in a `Map`, not in a data attribute.
      `updateAxGlyphResults` re-parses `tripletAttestations`, re-renders and re-stringifies the whole group per arrival; the type branch does the same.
- [ ] Replace the `{{variables}}` regex in `setupPromptGlyph` with `indexOf`.
      It runs on the execute path, and regex is banned.

## Pending, in flight, dead

- [ ] Start the palette migration, or delete the claim from all three places.
      Root `CLAUDE.md`, `docs/vision/glyphs.md` and the five-step plan in `default-glyphs.ts`; the palette is still twelve `palette-cell` buttons in `index.html`.
- [ ] Fix the `is` cell in `index.html`, or generate the palette markup.
      It reads `==` where `sym.IS` is `=`; `initializeSymbolPalette` rewrites every cell from generated sym, so only the file disagrees.
- [ ] Start the `sym` → `glyph/sym` move, or delete the claim.
      Root `CLAUDE.md` and `docs/vision/glyphs.md`; `glyph/` holds handlers, proto and storage.
- [ ] Migrate `prompt-glyph` to GlyphUI, or delete the TODO.
      The TODO names py and ts as the model; they adopted `ui.glyph()` and nothing else.
- [ ] Migrate `config-panel.ts` off `BasePanel`.
      `BasePanel`'s deprecation notice names `ConfigPanel` as the one blocker; both stylesheets are still linked in `index.html`.
- [ ] Give `qntx-atproto` a `ModulePath`.
      Its `RegisterGlyphs` declares `ContentPath` and no `ModulePath`, which holds the legacy path open.
- [ ] Port `'py can append to se'` to the package copy, then delete the web copy.
      Web 59 tests, package 58; deleting today drops that one.
- [ ] Delete the `'stream'` symbol, its two CSS rules and its restore branch.
      The registry entry, the `canvas-stream-glyph` rules in `followup.css`, and the branch in `renderGlyph`; nothing reports when the population reaches zero.
- [ ] Delete `spawnOnCanvas`, or make `spawnTypeGlyph` call it.
      `spawnOnCanvas` has no callers; all three glyphs its docstring names call `spawnOnCanvasDragging`.
- [ ] Delete `'ax'` and `'cursor'` from the `manifestationType` union.
      Never set, never read; `glyphRun` branches on `'panel'` and `'canvas'` and treats the rest as `'window'`.
- [ ] Unexport the seven file-internal exports.
      `buildResultTitleBar`, `subscribeStream`, `toggleColorMode`, `spawnFollowUpResult`, `spawnTripletGlyph`, `spawnSigmaGlyph`, `QUERY_COLOR_STATES`.
- [ ] Export `setupXGlyph` from the types conversion should reach.
      `convertNoteToPrompt` and `convertResultToNote` need it; only `setupNoteGlyph` and `setupPromptGlyph` export it, so those are the only destinations.
- [ ] Move `convertErrorToPrompt` into `conversions.ts`.
      It is private to the error glyph, outside the module that owns conversion.
- [ ] Register `'result'` and delete the two if/else branches ahead of the registry lookup.
      `renderGlyph` special-cases `'result'` and `'stream'`; `getGlyphTypeBySymbol('result')` returns undefined today.
- [ ] Migrate stored result content and delete the branch.
      `renderGlyph` reads `.result` and raw `ExecutionResult`, and its error path names a migration bug as the cause of a third case.
- [ ] Store the ax query and the se query in one encoding.
      `createAxGlyph` writes a raw string; `createSemanticGlyph` writes JSON with a legacy-string fallback in two places.
- [ ] Migrate sigma aggregates and delete the branch.
      `parseDistillAttrs` reads `{frequencies, count}` and legacy `{values, count}`.
- [ ] Report what the composition cleanup deleted.
      `buildCanvasWorkspace` removes compositions with no `edges` array on load, silently.
- [ ] Move prompt status into canvas state.
      `savePromptStatus` and `loadPromptStatus` keep it per glyph in `localStorage` — unsynced, unmigrated, outside the state everything else uses.
- [ ] Land or close `#765`.
      Focus manifestation is not in main; `packages/glyphs/manifestations/` has no focus.
- [ ] Close `#442`, `#461` and `#481`.
      Untouched since 2026-05-25, and what they built is in main.

## Landed on top

- [ ] Give `ceremony.ts` the button and input classes.
      `renderCeremony` and its `field` factory style inline; `web/ts` now has 39 buttons with no class, against `titlebar-btn` 11, `panel-btn` 10, `glyph-btn` 1.
- [ ] Give the 31 pill/badge/tile classes a shared base.
      `handlers-pill` and its four modifiers are the fifth family, after `as-meta-pill`, `glyph-meta-pill`, `panel-badge-*` and `pulse-badge-*`.
- [ ] Escape quotes in `escapeHtml`, or add `escapeAttr` and use it at the fifteen attribute sites.
      `escapeHtml` sets `textContent` and reads `innerHTML`, which escapes `&`, `<`, `>` only. Plugin name, version, description, `value=` and `pattern=` are among the fifteen.
- [ ] Restore in `copyable` only when the element still reads `copied`.
      `installCopyable` restores unconditionally at 1000 ms, and `copyable` is applied to the auth status line the sign-in flow rewrites at fourteen points.
- [ ] Route the `result-glyph` and `error-glyph` copy buttons through `copyable`.
      Three mechanisms now: `copyable` swaps text for 1000 ms, `headerBtn`'s copy swaps the icon for 1500 ms, `createErrorGlyph`'s does neither.
- [ ] Use the generated `User` types, or stop generating them.
      The generated `user.ts` declares `User`, `Key`, `Account`, `Binding` and `AccessLevel` and has no importer; `AccessLevel.SUPER` appears only there and in three comments.
- [ ] Define a namespace proto, or record that the hand-declared types are deliberate.
      `namespaces-view.ts` declares `Namespace` and `NamespaceOwner` by hand; no proto defines one.
- [ ] Add `bun run lint` to `ts.yml`.
      `make lint` exists and `make test` depends on it; no workflow runs it.
- [ ] Add `no-restricted-syntax` selectors for the regex ban and interpolated `innerHTML`.
      The two rules this document found broken are the two the config does not cover.

## Tool output

`knip` over `web/`, entry `ts/main.ts` plus build scripts and tests, `ts/generated/**` ignored. Exit 1.

- [ ] Delete the six unreachable files.
      `accessibility.ts`, `api/canvas-export.ts`, `embedding-store.ts`, `local-semantic-search.ts`, `filetree/navigator.ts`, `pulse/ats-node-view.ts`. The last two of those six are an island: one imports the other and nothing imports either.
- [ ] Check `exportCanvasDOM` against the live export before deleting it.
      The Export button calls `exportCanvasStatic` server-side; `exportCanvasDOM` renders client-side and is the dead one.
- [ ] Unexport `RESULT_ROW_TINT` and the six barrel re-exports.
      `attestation-result-row` exports the tint; `attestation-glyph` re-exports six symbols from `attestation-attrs` for nobody.
- [ ] Remove `**/*.test.ts` from the tsconfig exclude.
      `tsc --listFiles` puts 0 test files in the program, which is why the next two go unnoticed.
- [ ] Rewrite the type-definition-window test onto `bun:test`.
      It imports `vitest`, which is not installed and not in `package.json`, and passes anyway.
- [ ] Fix the `./glyph.ts` import in the glyph-run DOM test.
      The module does not exist; the import is type-only, so it is erased before it can fail.
- [ ] Add `markdown-it` to `package.json`.
      `note-markdown` imports it and it resolves only through `prosemirror-markdown`.
- [ ] Drop the default export of `uiState`.
      `state/ui` exports the same value named and as default.
