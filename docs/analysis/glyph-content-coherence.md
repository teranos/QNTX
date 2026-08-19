# Glyph Content Coherence

Glyph chrome is unified: `canvasPlaced()` owns the container, `.glyph-title-bar` was averaged from
six implementations, `wireExpandToWindow()` replaced the copy-pasted expand handler. Everything
below the title bar has had no such pass. Each box is the change; the line under it is what shows
the need and what makes it more than a substitution.

## Vocabularies

- [ ] Name the one vocabulary glyph content uses, in `web/CLAUDE.md`.
      Four are live: CSS classes + `innerHTML`, inline `el()` styles, GlyphUI primitives, server HTML.
- [ ] Adopt `ui.input`/`ui.button`/`ui.statusLine` in one glyph, or delete them.
      `glyph-ui.ts:120-170` — no consumer under `web/ts/`; only `qntx-plugins/ix-json/web/glyph-module.ts:40-58`.
- [ ] Collapse `.glyph-content` into `.glyph-content-area`.
      Nine glyphs use one, four the other, and `prompt-glyph.ts:320` and `note-glyph.ts:174` use neither.
- [ ] Delete the `content_url` rendering path.
      `plugin-glyph.ts:137-160` injects server HTML and re-runs its `<script>` tags; blocked below.

## Missing primitives

- [ ] Add a `spawn` option to `attestationResultRow`, then route the three hand-built rows through it.
      Blocked on the hardcoded `spawnAttestationGlyph` at `attestation-result-row.ts:58-61`; each row opens a different glyph.
- [ ] Move the dot-separated fact list into `attestation-result-row.ts`.
      Written three times: `type-result-line.ts:117-153`, `sigma-glyph.ts:657-685`, `result-glyph.ts:107-119`.
- [ ] Extract the key/value block out of `renderAttestationAttrs`.
      `attestation-attrs.ts:459-516` repeats it four times inside the one function.
- [ ] Give the stacked key/value layout a class, or make the attestation block use `.glyph-row`.
      `.glyph-row` (`window.css:249-262`, 17 uses) is two-column `space-between`; the attestation block stacks.
- [ ] Extract the editor half of py and ts; leave execution in each glyph.
      Height calc, CodeMirror boot, autosave, `(element as any).editor` and the error fallback are identical.
- [ ] Extract the query-glyph shell.
      `ax-glyph.ts:96-290` and `semantic-glyph.ts:208-345` repeat input, 500 ms debounce, `watcher_upsert` lifecycle, resize observer.
- [ ] Extract the attestation-family shell and pick one container background.
      `attestation-glyph.ts:100-160`, `triplet-glyph.ts:386-446`, `type-glyph.ts:165-215`, `sigma-glyph.ts:498-545`; triplet uses a different `rgba`.
- [ ] Pick one copy character and one dismiss character.
      Copy: `⎘` at `result-glyph.ts:335`, `📋` at `error-glyph.ts:111`. Dismiss: `✕` at `error-glyph.ts:146`, `×` at `note-glyph.ts:123`.
- [ ] Write symbols as literals throughout, or as escapes throughout.
      Run is `'▶'` at `py-glyph.ts:68` and `'▶'` at `ts-glyph.ts:133`; copy differs the same way inside `result-glyph.ts`.
- [ ] Import `SO` from generated sym in `error-glyph.ts:124,135` and `canvas/action-bar.ts:66`.
      `prompt-glyph.ts` imports it; those three write the `⟶` literal.
- [ ] Export `headerBtn` and build the expand button through it.
      `result-glyph.ts:314-318` is three lines inside a function body; "Expand to window" is retyped in six files.
- [ ] Put the accent palettes on the registry entry or in `tokens.css`.
      Four carry the same accent/value/dim triple (`sigma-glyph.ts:21-25` and three more); `glyph-registry.ts:28-46` has no color field.
- [ ] Give `appendEmptyState` a fixed class and stop threading the name through callers.
      `query-glyph-states.ts:37-46`; callers pass a string and `querySelector` it back (`ax-glyph.ts:349`).
- [ ] Route the four ad-hoc empty states and three loading states through one helper.
      Empty: `result-glyph.ts:263`, `chart-glyph.ts:178`, `doc-glyph.ts:72`, `db-glyph.ts:252`. Loading: `ax-glyph.ts:134`, `chart-glyph.ts:134`, `plugin-glyph.ts:94`.
- [ ] Move `createLoadingState`, `createErrorState` and `parseError` out of `base-panel-error.ts`.
      `base-panel-error.ts:57-170`; that file is on `base-panel.ts`'s delete list and these are what glyph content lacks.
- [ ] Use `Button` for actions inside glyphs, or drop the rule from `web/CLAUDE.md`.
      `components/glyph/` has 29 raw `<button>` creations; `Button` is used at 8 sites in 3 files.
- [ ] Pick one presentation for a failed action inside a glyph.
      Five exist: `py-glyph.ts:112`, `py-glyph.ts:114-121`, `prompt-glyph.ts:132-175`, `query-glyph-states.ts:52-96`, `error-glyph.ts`.
- [ ] Pick one hover system.
      `has-tooltip` at 37 sites, `title=` at 15; the same run button uses `has-tooltip` at `prompt-glyph.ts:179` and `title=` at `py-glyph.ts:70`.
- [ ] Pick one status mechanism and drive it from a data attribute.
      Four exist (`query-glyph-states.ts:24-33`, `prompt-glyph.ts:132-175`, `glyph-ui.ts:146-170`, `execution-state.css`); three define their own colors.
- [ ] Merge `as-meta-pill` and `glyph-meta-pill`, then delete the suppression guard.
      `watcher-queue-status.ts:172` returns null on any glyph that already has an attestation pill.
- [ ] Extract the sync/connectivity dataset wiring into one call.
      Repeated at `ax-glyph.ts:274-285`, `semantic-glyph.ts:328-340`, `py-glyph.ts:179-187`, `ts-glyph.ts:245-250`.

## Reinventions

- [ ] Point `spawnTypeGlyph` at the shared spawn, or record that type glyphs place instantly on purpose.
      `type-glyph.ts:226-269` re-implements it; the other three call `spawnOnCanvasDragging` and place on the cursor.
- [ ] Build the result header once.
      `createResultGlyph` (`:293-345`) and `buildResultTitleBar` (`:981-1022`) have drifted; the second lost the color toggle.
- [ ] Extract `spawnResultBelow`.
      `prompt-glyph.ts:356-403`, `glyph-followup.ts`, `canvas-workspace-builder.ts`; `TODO [TS-5]` at `:353` is still open.
- [ ] Extract `collectMeldedAttachments`.
      `prompt-glyph.ts:216-252` and `glyph-followup.ts`; `TODO [TS-4]` at `:216` is still open.
- [ ] Use `GlyphProximity` for the thread reveal, or record why canvas needs its own.
      `thread-glyph.ts:50-62` hand-rolls a `Math.hypot` check against `REVEAL_RADIUS = 80`.
- [ ] Replace literal `monospace` and `'11px'` with the tokens.
      52 and 43 occurrences in `components/glyph/`; `--font-mono` is used 68× in CSS and once in `web/ts`.

## Defects

None of these is covered by the frontend test suite.

- [ ] Add `destroyGlyph(element, glyphId)` and call it from all three delete paths.
      `note-glyph.ts:162-167` skips `runCleanup`, so its own ✕ leaves the ProseMirror view, autosave and observer live.
- [ ] Register cleanup in py-glyph and ts-glyph.
      Neither calls `storeCleanup`; `runCleanup` runs an empty list and the `EditorView` and two subscriptions outlive the element.
- [ ] Build the meta popover with `textContent`.
      `buildMetaLines` (`attestation-glyph.ts:32-59`) interpolates `actors`, `source` and `id` raw into `innerHTML` at `:117, :287, :494`.
- [ ] Define the 36 undefined custom properties, or delete their call sites.
      42 referenced, 6 injected at runtime; the rest includes a whole second token vocabulary beside `tokens.css`.
- [ ] Define `--ats-editor-*`, or inline the fallbacks.
      Nine names, zero definitions, so `ats-code-block.css` renders entirely on its fallbacks.
- [ ] Define `--panel-border-color`.
      Its two fallbacks disagree about the theme: `#333` in `type-definition-window.css`, `#e0e0e0` in `window.css:253,390,411`.
- [ ] Define `--text-muted`, or give `result-glyph.ts:115` a fallback.
      With neither, the declaration is invalid at computed-value time and the stats line inherits its parent's color.
- [ ] Delete the `dataset.attestation` writes.
      Four writers, no reader (`attestation-result-row.ts:54` and three more); attestations holding PDB or FASTA serialise the file into the attribute.
- [ ] Hold the triplet group in a `Map`, not in a data attribute.
      `ax-glyph.ts:405-420` re-parses, re-renders and re-stringifies the whole group per arrival; `:376-400` the same.
- [ ] Replace the regex in `prompt-glyph.ts:192` with `indexOf`.
      `/\{\{[^}]+\}\}/.test(template)` on the execute path; regex is banned.

## Pending, in flight, dead

- [ ] Start the palette migration, or delete the claim from all three places.
      `CLAUDE.md:73`, `docs/vision/glyphs.md:180-190`, `default-glyphs.ts:12-50`; the palette is still twelve buttons at `index.html:296-308`.
- [ ] Fix `index.html:302` to `=`, or generate the palette markup.
      It renders `is` as `==` where `sym.IS` is `=`; init rewrites every cell from generated sym, so only the file disagrees.
- [ ] Start the `sym` → `glyph/sym` move, or delete the claim.
      `CLAUDE.md:71` and `docs/vision/glyphs.md:172`; `glyph/` holds handlers, proto and storage.
- [ ] Migrate `prompt-glyph` to GlyphUI, or delete the TODO.
      `prompt-glyph.ts:14` names py and ts as the model; they adopted `ui.glyph()` and nothing else.
- [ ] Migrate `config-panel.ts` off `BasePanel`.
      `base-panel.ts:1-8` names it as the one blocker; both stylesheets are still linked at `index.html:24,43`.
- [ ] Give `qntx-atproto` a `ModulePath`.
      `qntx-plugins/qntx-atproto/plugin.go:320-325` declares `ContentPath` only, which holds the legacy path open.
- [ ] Port `'py can append to se'` to the package copy, then delete the web copy.
      Web 59 tests, package 58; deleting today drops that one.
- [ ] Delete the `'stream'` symbol, its two CSS rules and its restore branch.
      `glyph-registry.ts:61`, `css/glyph/followup.css:24,87`, `canvas-workspace-builder.ts:318-323`; nothing reports when the population reaches zero.
- [ ] Delete `spawnOnCanvas`, or make `spawnTypeGlyph` call it.
      `spawn-on-canvas.ts:36` has no callers; all three glyphs its docstring names call the dragging half.
- [ ] Delete `'ax'` and `'cursor'` from the `manifestationType` union.
      `packages/glyphs/glyph.ts:21`; never set, never read, and `run.ts:187-196` branches on the other two.
- [ ] Unexport the seven file-internal exports.
      `buildResultTitleBar`, `subscribeStream`, `toggleColorMode`, `spawnFollowUpResult`, `spawnTripletGlyph`, `spawnSigmaGlyph`, `QUERY_COLOR_STATES`.
- [ ] Export `setupXGlyph` from the types conversion should reach.
      `conversions.ts:1-13` requires it; two of fourteen have it, so the module can only ever convert to note or prompt.
- [ ] Move `convertErrorToPrompt` into `conversions.ts`.
      It is private to `error-glyph.ts:249`, outside the module that owns conversion.
- [ ] Register `'result'` and delete the two if/else branches ahead of the registry lookup.
      `canvas-workspace-builder.ts:269-316` and `:318-323`; `getGlyphTypeBySymbol('result')` returns undefined today.
- [ ] Migrate stored result content and delete the branch.
      `canvas-workspace-builder.ts:289` reads two shapes; `:281` names a migration bug as the cause of a third case.
- [ ] Store the ax query and the se query in one encoding.
      `ax` writes a raw string, `se` writes JSON with a legacy-string fallback (`semantic-glyph.ts:52,62`) — the same title-bar input.
- [ ] Migrate sigma aggregates and delete the branch.
      `{frequencies, count}` vs legacy `{values, count}` at `sigma-glyph.ts:113,341`.
- [ ] Report what the composition cleanup deleted.
      `canvas-workspace-builder.ts:799-801` removes old-format compositions on load without telling anyone.
- [ ] Move prompt status into canvas state.
      `prompt-glyph.ts:43-66` keeps it in `localStorage` per glyph — unsynced, unmigrated, outside the state everything else uses.
- [ ] Land or close `#765`.
      Focus manifestation is not in main; `packages/glyphs/manifestations/` has no focus.
- [ ] Close `#442`, `#461` and `#481`.
      Untouched since 2026-05-25, and what they built is in main.

## Landed on top

- [ ] Make `namespaces-bar` a glyph, or record that top-level bars are not glyphs.
      `namespaces-bar.ts` builds from `innerHTML` with its own stylesheet at `index.html:31` — the palette's shape, added beside it.
- [ ] Give `ceremony.ts` the button and input classes.
      `ceremony.ts:39-70` styles inline; `web/ts` now has 39 buttons with no class, against `titlebar-btn` 11, `panel-btn` 10, `glyph-btn` 1.
- [ ] Give the 31 pill/badge/tile classes a shared base.
      `handlers-pill` and its four modifiers (`css/handlers-panel.css:140-182`) are the fifth family.
- [ ] Escape quotes in `escapeHtml`, or add `escapeAttr` and use it at the fifteen attribute sites.
      `html-utils.ts:21-24` escapes `&`, `<`, `>` only; `plugin-panel.ts:428,429,589,627-632,645` and four more put plugin strings in attributes.
- [ ] Restore in `copyable` only when the element still reads `copied`.
      `copyable.ts:35-40` restores unconditionally at 1000 ms; `auth-glyph.ts:161` applies it to a line the sign-in flow rewrites at fourteen points.
- [ ] Route the `result-glyph` and `error-glyph` copy buttons through `copyable`.
      Three mechanisms now: `copyable` (1000 ms text swap), `result-glyph.ts:335` (1500 ms icon swap), `error-glyph.ts:111`.
- [ ] Use the generated `User` types, or stop generating them.
      `generated/proto/plugin/grpc/protocol/user.ts` — 155 lines, no importer; `AccessLevel.SUPER` appears only there and in three comments.
- [ ] Define a namespace proto, or record that the hand-declared types are deliberate.
      `namespaces-view.ts:3-13` declares `Namespace` and `NamespaceOwner`; no proto defines one.
- [ ] Add `bun run lint` to `ts.yml`.
      `make lint` exists and `make test` depends on it (`Makefile:190-196`); no workflow runs it.
- [ ] Add `no-restricted-syntax` selectors for the regex ban and interpolated `innerHTML`.
      The two rules this document found broken are the two the config does not cover.

## Tool output

`knip` over `web/`, entry `ts/main.ts` plus build scripts and tests, `ts/generated/**` ignored. Exit 1.

- [ ] Delete the six unreachable files.
      `accessibility.ts`, `api/canvas-export.ts`, `embedding-store.ts`, `local-semantic-search.ts`, `filetree/navigator.ts`, `pulse/ats-node-view.ts`.
- [ ] Check `exportCanvasDOM` against the live export before deleting it.
      The Export button (`canvas-expanded.ts:114-128`) calls `exportCanvasStatic` server-side; the dead one renders client-side.
- [ ] Unexport `RESULT_ROW_TINT` and the six barrel re-exports.
      `attestation-result-row.ts:13`; `attestation-glyph.ts:26` re-exports from `attestation-attrs` for nobody.
- [ ] Remove `**/*.test.ts` from the tsconfig exclude.
      `tsc --listFiles` puts 0 test files in the program, which is why the next two go unnoticed.
- [ ] Rewrite `type-definition-window.test.ts:1` onto `bun:test`.
      It imports `vitest`, which is not installed and not in `package.json`, and passes anyway.
- [ ] Fix the `./glyph.ts` import at `run.dom.test.ts:11`.
      The module does not exist; the import is type-only, so it is erased before it can fail.
- [ ] Add `markdown-it` to `package.json`.
      `prose/note-markdown.ts:9` imports it and it resolves only through `prosemirror-markdown`.
- [ ] Drop the default export of `uiState`.
      `state/ui.ts` exports the same value named and as default.
