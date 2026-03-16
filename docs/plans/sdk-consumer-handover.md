# GlyphUI SDK: Consumer-Ready Handover

Work done on `claude/review-main-changes-4ifyl`. Point 1 (container content area) is complete — `container()` now returns `{ element, titleBar, content }` and ix-net uses it as a live capture viewer.

Remaining points below, in priority order for making the SDK the canonical path for external plugin authors.

---

## 1. Button variants (#4)

`web/ts/components/button.ts` has a full `Button` class with variants (primary, danger, ghost, etc.), sizes, loading/error states, and a registry. None of this is exposed through the SDK — `ui.button()` only takes `{ label, primary?, onClick }`.

**Simple**: Expose the existing `Button` class through `ui.button()` by forwarding `variant`, `size`, and `disabled` options. Three lines in `glyph-ui.ts`, zero new code.

**Advanced**: Add state-driving methods (`setLoading`, `setError`) to the returned button handle so plugins can show async feedback without managing DOM classes themselves.

---

## 2. statusLine CSS (#6)

`ui.statusLine()` creates a div and returns `{ show(), clear() }`, but applies no styles — the element is unstyled unless the plugin adds its own CSS. An SDK primitive that ships without a visual identity is incomplete.

**Simple**: Add inline styles in `statusLine()` matching the existing pattern (monospace, small text, color-coded error/success). Self-contained, no CSS file needed.

**Advanced**: Define `.glyph-status-line` in the SDK stylesheet with token-backed colors (`--color-error`, `--color-success`), transition on show/hide, and a severity parameter (info/warn/error) that maps to distinct visual treatments.

---

## 3. State classes (#3)

`query-glyph-states.ts` has `createColorStateSetter()` and `QUERY_COLOR_STATES` — a pattern for tinting container + titleBar based on state (idle, pending, success, error). Every glyph that shows async state reimplements this by hand.

**Simple**: Add `ui.setState(name)` that applies the triple-color pattern (container bg, section bg, text) from `QUERY_COLOR_STATES` to the container returned by `ui.glyph()`. One function, reuses existing palette.

**Advanced**: Make state a first-class concept on the container — `ui.glyph()` accepts a `states` map, returns a `setState(name)` handle, and transitions between states with the CSS transition tokens already in `tokens.css`.

---

## 4. cssText to classes (#5)

Internal glyphs (query, ix-json, ix-net) build styles with `el.style.cssText = '...'` string concatenation. Fragile, unreadable, and impossible to theme.

**Simple**: Extract the most repeated `cssText` patterns (scrollable list, header row, monospace content) into utility classes in a shared stylesheet. Migrate internal glyphs one at a time.

**Advanced**: Create a small set of SDK layout classes (`glyph-scroll`, `glyph-header`, `glyph-row`, `glyph-mono`) backed by design tokens, and document them as the canonical way to build glyph interiors. Plugin authors get consistent styling without writing CSS.

---

## 5. Triple color tokens (#2)

Glyph status colors exist in `tokens.css` as `--glyph-status-{running,success,error}-{bg,section-bg,text}` but query glyphs define their own parallel palette in `QUERY_COLOR_STATES` with hardcoded hex values that don't reference these tokens.

**Simple**: Replace the hex values in `QUERY_COLOR_STATES` with `var()` references to the existing `--glyph-status-*` tokens. Single-file change, tokens already exist.

**Advanced**: Unify the two palettes — extend `tokens.css` with `idle` and `pending` states (currently only in the JS palette), delete `QUERY_COLOR_STATES`, and have `ui.setState()` read directly from CSS custom properties. One source of truth.
