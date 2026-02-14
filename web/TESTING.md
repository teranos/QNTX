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

**DOM Testing** — `test-setup.ts` creates happy-dom or JSDOM based on `USE_JSDOM=1`. Gate DOM tests with the skip pattern in `*.dom.test.ts` files.

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

**Callbacks**:
```typescript
const mockCallback = mock((path: string) => {});
expect(mockCallback).toHaveBeenCalledWith('expected-path.md');
```

See `ts/prose-navigation.test.ts` for complete examples.
