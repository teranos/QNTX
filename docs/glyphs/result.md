# Result Glyph

Execution output display. Shows stdout, stderr, and errors from glyph execution.

## Spawning

Result glyphs are not user-spawned. They are created automatically when a py, ts, or prompt glyph executes. The result auto-melds below its parent glyph via `autoMeldResultBelow`.

## Structure

- **Header**: duration label (`{n}ms`), expand-to-window button (not yet implemented, #440), close button
- **Output area**: monospace `pre-wrap` container showing stdout (default color), stderr (error color), errors (bold error color)
- Height auto-calculated from line count, clamped to 80–400px

## Close behavior

Closing a result that is inside a melded composition unmelds the entire composition first (via `unmeldComposition`), then removes the result element.

## Meld position

Result is a terminal node — it has no outgoing ports in the meld registry. It receives `bottom` connections from py and prompt glyphs.

## Content persistence

The `ExecutionResult` object is JSON-serialized into the glyph's `content` field for persistence across page reloads. `updateResultGlyphContent` updates an existing result in-place when re-execution occurs.

## Files

| File | Role |
|------|------|
| `web/ts/components/glyph/result-glyph.ts` | Glyph factory, `createResultGlyph`, `updateResultGlyphContent` |
| `web/ts/components/glyph/meld/auto-meld-result.ts` | Auto-meld helper used by py/ts/prompt glyphs |
| `web/ts/components/glyph/result-glyph.dom.test.ts` | DOM structure tests |
| `web/ts/components/glyph/result-drag.test.ts` | Drag behavior tests |
