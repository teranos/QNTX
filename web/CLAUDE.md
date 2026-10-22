# Web Frontend

## Element Axioma

An element is exactly ONE DOM element for its entire lifetime (@teranos/elements AXIOMAS.md). FORBIDDEN: cloneNode, createElement for an existing element, re-rendering via diffing, two elements with the same data-element-id. ALLOWED: reparenting, transform changes, delaying content mount until morph completes.

All tray creation via `tray.add`; canvas via `canvasPlaced`. Register new types in `components/element/element-registry.ts`.

## Tables

**A table is @teranos/elements'.** "The elements package should own Tables" "TABLES BEING MAX 4 col". Build one with `createTable(name, heads, rows)`: one to four heads, every row as wide, or it does not compile. A table built by hand (`createElement('table' | 'thead' | 'tbody' | 'tr' | 'th')`, or table markup in a string) stops the build (ESLint `NO_RAW_TABLE`). A cell may be the host's own `td`.

## Controls

**A control is @teranos/elements'.** A button is `pressFrom`: the same size at every stage, white at rest, red armed only when its second press cannot be undone (`irrevocable`), green and shimmering armed only when it creates (`creates`); no colour, size or variant to choose, and misuse does not compile. A `button`, `input`, `select` or `textarea` made by hand (`createElement`, or markup in a string), or an import of `components/button`, stops the build (ESLint `NO_RAW_CONTROL`). What stands is in `eslint-suppressions.json` and only falls. The rules: the Press in the Design Element (`ts/design-element.ts`).

## WASM

`ats-wasm.ts` wraps the browser WASM module (crates/ats-wasm). Must call `initialize(dbName)` before any WASM operation except `axFilter` (synchronous, no init needed).

WASM files live in `web/wasm/` and must be built (`make ats`) before `make build`.

## State

`uiState` (state/ui.ts) is THE singleton — pub/sub reactivity. Canvas glyph state syncs to backend via `api/canvas-sync.ts`.

## WebSocket

Handlers register via `registerHandler(type, handler)` and MUST `unregisterHandler` on cleanup. Built-in handlers are in `MESSAGE_HANDLERS` (client/ws.ts). Specialized handlers in `websocket-handlers/`.

## CSS

Dark-mode first. Design tokens in `tokens.css`. **`!important` is banned.** Fix specificity at the source.

**Text size is @teranos/elements'.**

"There's no doubt in my mind that Elements owns it's own font sizes."

TypeScript writes no size: inside an element, `sizeText(node, 'xs' | 'sm' | 'md' | 'lg')`; ESLint (`NO_FONT_SIZE`) stops any other. A stylesheet or `index.html` picks one of the package's sizes by its token, `font-size: var(--font-size-xs | sm | md | lg | field)`, and `tokens.css` says the package's number for each; `font-size-ban.ts` stops anything else, a token off the package's number among them. Both run in `bun run lint`.

Z-index hierarchy:

- Loading screen: 200000
- Brow (.brow, Dynamic Island status line): 100004 — above the door: a shut door still shows the node's vitals
- Door (#door, the identity threshold in the system bar): 100003
- Glyph tray (.glyph-run): 100002
- Toast, and a refusal said over the canvas (.refusal-say): 100001
- Panel fullscreen: 100000
- Windows: 10003 and up — each one raised above the last, so the one you just
  opened or touched is in front, including in front of a panel (z-order.ts)
- System drawer: 10003
- Panel glyphs: 10002
- Canvas controls/toolbar: 10001
- Canvas fullscreen: 10000

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

- **A press:** throw from `does` → the failure is said beside it (`pressFrom`, @teranos/elements)
- **Form validation:** Inline messages near fields
- **API errors:** `log.error()` to console, display in UI context where action occurred

**CRITICAL WORKFLOW:**

1. Check what component you're modifying
2. Read that component's source file FIRST
3. Use its built-in capabilities
4. Never add generic error handling (alert/toast) without checking component API first

---

## UI: Underline means you are leaving

**An underline is an external link and nothing else.** Text with a line under it
opens something outside QNTX. Nothing internal is underlined — not a press that
opens a glyph, not a toggle, not a pressable value — so the line under text is
the whole of the warning that the node is about to stop being where you are.

---

## UI: No Ellipsis

**NEVER use `text-overflow: ellipsis`.** All text wraps — data is never hidden behind truncation. Use `word-break: break-word` and `overflow-wrap: break-word` for wrapping. This applies everywhere: CSS, inline styles, all UI components.

---

## Symbols

The symbol is the UI's: the one place a segment is mapped to a glyph (see Symbols in [CLAUDE.md](../CLAUDE.md)).
