# Web UI Testing

Uses [Bun Test](https://bun.sh/docs/cli/test) - Jest-compatible API, 10-100x faster.

Don't test implementation details. Organize tests by persona: **Tim** (happy path), **Spike** (edge cases), **Jenny** (complex scenarios).

## Quick Start

```bash
bun test              # Run once
bun test --watch      # Watch mode
make test-web         # From project root
USE_JSDOM=1 bun test path/to/test.dom.test.ts  # Run single JSDOM test locally
```

## File Organization

Tests co-located with source using `.test.ts` suffix:
```
ts/prose-navigation.ts
ts/prose-navigation.test.ts
```

## Key Patterns

**DOM Testing** — `test-setup.ts` provides happy-dom by default or JSDOM when `USE_JSDOM=1` is set. Tests using basic DOM APIs (document.createElement, etc.) work as regular `.test.ts` files with happy-dom. Tests requiring full browser APIs (Web Animations API, ResizeObserver) use `.dom.test.ts` with the JSDOM skip pattern.

**Width** — neither test DOM lays anything out, so an element wider than its screen is measured in a real browser and nowhere else, against the node as it is: `browser/live.browser.ts` opens every element of `ts/specimens.ts` on a Pixel 7, an iPhone 15, a desktop and a desktop 320 CSS px wide ([WCAG 2.2, 1.4.10 Reflow](https://www.w3.org/TR/WCAG22/#reflow)), every call answered by the node as its CI user, and names each element with content that cannot be reached, each that opens at its end (`data-opens-at`) short of it or with its field (`data-say`) off the bottom line, each target under 24×24 CSS px whose circle meets another ([2.5.8 Target Size (Minimum)](https://www.w3.org/TR/WCAG22/#target-size-minimum)), and each disabled control. What is not yet met is owed in `browser/owed`, as nilcheck's baseline is: a number may fall and may not rise. What was not measured, an element that did not stand or a call the node refused, is never owed. There are no fixtures. The deployment that owns a node runs it (`.github/workflows/live-gate.yml`) and says the verdict as the commit's `live` status (`live-verdict.yml`).

"if it overflows width, hard fail, die"

**localStorage Mocking** (see `prose-navigation.test.ts`):
```typescript
const mockLocalStorage = (() => {
    let store: Record<string, string> = {};
    return {
        getItem: (key: string) => store[key] || null,
        setItem: (key: string, value: string) => { store[key] = value; },
        clear: () => { store = {}; }
    };
})();
```

**`mock.module` is process-global** — mocks leak across test files in the same `bun test` run; if two files mock the same module, the last one wins and can change async behavior (e.g., turning a throwing call into a real `await`).

**Connectivity mock** — `test-setup.ts` (preloaded via `bunfig.toml`) provides a base `connectivityManager` mock with all interface methods (`state`, `subscribe`, `subscribeAuth`). Tests that need custom behavior (e.g. tracking subscribers, simulating offline) override it with their own `mock.module` call — but **must include all interface methods** in their override, otherwise other test files that import connectivity transitively will break when execution order differs (local vs CI).

**Callbacks**:
```typescript
const mockCallback = mock((path: string) => {});
expect(mockCallback).toHaveBeenCalledWith('expected-path.md');
```

See `ts/prose-navigation.test.ts` for complete examples.
