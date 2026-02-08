# Web UI Testing

Uses [Bun Test](https://bun.sh/docs/cli/test) - Jest-compatible API, 10-100x faster.

Don't test implementation details. Organize tests by persona: **Tim** (happy path), **Spike** (edge cases), **Jenny** (complex scenarios).

## Quick Start

```bash
bun test              # Run once
bun test --watch      # Watch mode
make test-web         # From project root
```

## File Organization

Tests co-located with source using `.test.ts` suffix:
```
ts/prose-navigation.ts
ts/prose-navigation.test.ts
```

## Key Patterns

**DOM Testing** (happy-dom for fast tests, JSDOM for complex browser APIs):
```typescript
// Fast tests - use happy-dom (automatic)
const panel = document.createElement('div');
nav.bindElements(panel);

// Complex tests requiring browser APIs - gate with USE_JSDOM=1 (see *.dom.test.ts files)
```

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

**Callbacks**:
```typescript
const mockCallback = mock((path: string) => {});
expect(mockCallback).toHaveBeenCalledWith('expected-path.md');
```

See `ts/prose-navigation.test.ts` for complete examples.
