# Web Frontend

## Glyph Axiom

A glyph is exactly ONE DOM element for its entire lifetime. FORBIDDEN: cloneNode, createElement for existing glyph, re-rendering via diffing, two elements with same data-glyph-id. ALLOWED: reparenting, transform changes, delaying content mount until morph completes.

All creation via `createGlyphElement` factory in `glyph/run.ts`. Register new types in `glyph/glyph-registry.ts`.

## WASM

`qntx-wasm.ts` wraps the browser WASM module (crates/qntx-wasm). Must call `initialize(dbName)` before any WASM operation except `parseQuery` (synchronous, no init needed).

Provides: query parsing, IndexedDB attestation CRUD, fuzzy search, classification. WASM files live in `web/wasm/` and must be built (`make wasm`) before `make build`.

## State

`uiState` (state/ui.ts) is THE singleton — pub/sub reactivity, localStorage persistence. Canvas glyph state syncs to backend via `api/canvas-sync.ts`.

## WebSocket

Handlers register via `registerHandler(type, handler)` and MUST `unregisterHandler` on cleanup. Built-in handlers are in `MESSAGE_HANDLERS` (websocket.ts). Specialized handlers in `websocket-handlers/`.

## CSS

Dark-mode first. Variables in `core.css`. Z-index hierarchy:
- Loading screen: 200000
- Glyph tray (.glyph-run): 100001
- Panel fullscreen: 100000
- System drawer: 10002
- Canvas: 10000
- Toast: 9999

## Build

`build.ts` bundles into `internal/server/dist/` for Go embedding. Entry point: `ts/main.ts`. WASM `.wasm` files are copied to `dist/js/` (import.meta.url resolution). Build fails if no `.wasm` files found.

## Testing

`mock.module` is process-global — mocks leak across test files in the same `bun test` run. If two files mock the same module, the last one wins.

## Bun Bundler: const cross-references are unsafe

`const BAR = FOO` where FOO is another const → BAR becomes `undefined` in bundle. Always use literal values for module-scope constants.
