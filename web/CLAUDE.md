# Web Frontend

## Glyph Axiom

A glyph is exactly ONE DOM element for its entire lifetime. FORBIDDEN: cloneNode, createElement for existing glyph, re-rendering via diffing, two elements with same data-glyph-id. ALLOWED: reparenting, transform changes, delaying content mount until morph completes.

All creation via `createGlyphElement` factory in `glyph/run.ts`. Register new types in `glyph/glyph-registry.ts`. The symbol palette is being migrated to the GlyphRun tray.

## WASM

`ats-wasm.ts` wraps the browser WASM module (crates/ats-wasm). Must call `initialize(dbName)` before any WASM operation except `parseQuery` (synchronous, no init needed).

WASM files live in `web/wasm/` and must be built (`make ats`) before `make build`.

## State

`uiState` (state/ui.ts) is THE singleton — pub/sub reactivity. Canvas glyph state syncs to backend via `api/canvas-sync.ts`.

## WebSocket

Handlers register via `registerHandler(type, handler)` and MUST `unregisterHandler` on cleanup. Built-in handlers are in `MESSAGE_HANDLERS` (client/ws.ts). Specialized handlers in `websocket-handlers/`.

## CSS

Dark-mode first. Design tokens in `tokens.css`. **`!important` is banned.** Fix specificity at the source.

Z-index hierarchy:

- Loading screen: 200000
- Brow (.brow, Dynamic Island status line): 100004 — above the door: a shut door still shows the node's vitals
- Door (#door, the identity threshold in the system bar): 100003
- Glyph tray (.glyph-run): 100002
- Toast: 100001
- Panel fullscreen: 100000
- System drawer: 10003
- Panel glyphs: 10002
- Canvas controls/toolbar: 10001
- Canvas fullscreen: 10000
- Windows: 9999

## Logging

Browser `console.*` and `log.*` calls are forwarded to `tmp/qntx-{port}.log` (prefixed `[Browser]`).

## Build

`build.ts` bundles into `internal/server/dist/` for Go embedding. Entry point: `ts/main.ts`. WASM `.wasm` files are copied to `dist/js/` (import.meta.url resolution). Build fails if no `.wasm` files found.

## Testing

`mock.module` is process-global — mocks leak across test files in the same `bun test` run. If two files mock the same module, the last one wins.

## Bun Bundler: const cross-references are unsafe

`const BAR = FOO` where FOO is another const → BAR becomes `undefined` in bundle. Always use literal values for module-scope constants.

## Error Handling — READ THIS FIRST

**BANNED (ESLint enforced): `alert()`, `confirm()`, `prompt()`, `toast()`, raw `fetch()`, and unbound catches** — every `catch` must bind the error it caught (`catch (err)`, never `catch {`), and every `.catch()` handler must take the rejection. A catch that cannot reference its error can only swallow it.

Use `apiFetch`/`apiJson` from `./client`. Exemptions are in `eslint.config.js`.

Use contextualized error display:

- **Button component:** Throws from `onClick` → automatic slide-out error display (see components/button.ts)
- **Form validation:** Inline messages near fields
- **API errors:** `log.error()` to console, display in UI context where action occurred

**CRITICAL WORKFLOW:**

1. Check what component you're modifying
2. Read that component's source file FIRST
3. Use its built-in capabilities
4. Never add generic error handling (alert/toast) without checking component API first

---

## UI: No Ellipsis

**NEVER use `text-overflow: ellipsis`.** All text wraps — data is never hidden behind truncation. Use `word-break: break-word` and `overflow-wrap: break-word` for wrapping. This applies everywhere: CSS, inline styles, all UI components.

---

## Glyphs

Glyphs ⧉  are the universal UI primitive. Symbols (`sym` package) are the visual expression of a glyph — through a sym, a glyph can be expressed. The `sym` package will become a subpackage of `glyph/` (`glyph/sym`).

The symbol palette is being migrated to the GlyphRun tray — each palette action becomes a glyph with its own manifestation type.

See [GLOSSARY.md](docs/GLOSSARY.md) for symbol definitions and [packages/glyphs/VISION.md](packages/glyphs/VISION.md) for the architectural vision.
