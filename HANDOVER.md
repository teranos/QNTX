# HANDOVER — chart-glyph branch

## Status

PR #813 open. Branch has chart glyph on canvas with dummy D3 data, top-direction meld support (detection + glow proven), single-edge directional shadows. Tests pass (663).

## Blocker

**Composition jump on top meld.** When chart melds above ax, ax shifts down. Root cause: `performMeld` in `packages/glyphs/meld/meld-composition.ts` positions the composition at the initiator's canvas position, but `applyColumnLayout` places the initiator at a non-zero internal offset for `top` direction (row 2 instead of row 1). Three compensation attempts failed — the math is simple (`origY - internalOffset`) but something diverges at runtime. Next step: add `console.log` in `performMeld` to dump `origY`, `internalY`, `scale`, final `composition.style.top`. See `docs/glyphs/top-meld-jump.md`.

## Roadmap (sequenced)

1. **Jump bug** — `packages/glyphs/meld/meld-composition.ts` lines ~209 and ~349
2. **Wire chart to ax data** — replace dummy data with watcher pipeline (ax->py pattern in `web/ts/components/glyph/glyph-followup.ts`)
3. **WASD attribute cycling** — keydown on selected chart, W/S cycles y-axis attribute, re-renders live, axis label is feedback
4. **Comma/period chosen attributes** — comma promotes previewing to chosen (persistent line), period demotes, state in glyph `content` field
5. **Arrow keys viz type** — Up/Down cycles line/area/bar/scatter, orthogonal to WASD
6. **A/D time window** — adjusts x-axis range (hour/day/week/month), changes watcher query
7. **Sigma as data source** — add sigma `top` port in meldability.ts, same watcher mechanism
8. **Outgoing ports** — chosen attributes as chart output, add ports to `MELDABILITY['canvas-chart-glyph']`

## What's proven

- Chart glyph renders, spawns from tray, headerless fold bar, resize-aware D3
- `canvas-chart-glyph` in `ALL_GLYPH_CLASSES`, ax has `top` port targeting chart
- Top direction glow: single-edge shadows on correct edges
- Top direction detection: test passes, meld triggers on mouse release
- Left-to-right meld glow improved (single-edge, no bleed)

## Key files

- `web/ts/components/glyph/canvas-chart-glyph.ts` — chart glyph renderer
- `web/ts/components/glyph/glyph-registry.ts` — spawn menu registration
- `packages/glyphs/meld/meldability.ts` — port rules
- `packages/glyphs/meld/meld-feedback.ts` — directional shadows
- `packages/glyphs/meld/meld-composition.ts` — jump bug lives here
- `packages/glyphs/meld/meld-detect.ts` — top direction proximity check
- `packages/glyphs/edge-graph.ts` — `computeGridPositions` grid normalization
- `docs/glyphs/chart.md` — vision (keyboard nav, attribute states, modes)
- `docs/glyphs/top-meld-jump.md` — jump bug analysis
