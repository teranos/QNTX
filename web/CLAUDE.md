# Web Frontend

## Bun Bundler: const cross-references are unsafe

Bun's bundler silently drops assignments where one module-scope `const` references another:

```typescript
const FOO = 36;
const BAR = FOO;  // BAR becomes undefined in the bundle
```

Bundled output:
```javascript
var FOO = 36, BAR, ...  // assignment gone
```

This is Bun-specific (esbuild/webpack/rollup handle this correctly). It produces `undefined` at runtime with no build error or warning. Use literal values for all module-scope constants — never `const X = Y`.
