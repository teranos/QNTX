# Generated CSS

This directory contains CSS files auto-generated from Go source code.

## sym.css - Symbol Custom Properties

Generated from `sym/symbols.go`, providing CSS custom properties for QNTX symbols.

### Usage

Import in your CSS:

```css
@import './generated/sym.css';

/* Use symbols in content */
.segment-icon::before {
  content: var(--sym-i);
}

.pulse-indicator::before {
  content: var(--sym-pulse);
}
```

### Available Variables

All QNTX symbols are available as CSS custom properties with `--sym-` prefix:

- `--sym-i` - ⍟ (Self)
- `--sym-am` - ≡ (Configuration)
- `--sym-ix` - ⨳ (Ingest)
- `--sym-ax` - ⋈ (Expand)
- `--sym-by` - ⌬ (Actor)
- `--sym-at` - ✦ (Temporal)
- `--sym-so` - ⟶ (Therefore)
- `--sym-pulse` - ꩜ (Pulse system)
- `--sym-pulse-open` - ✿ (Startup)
- `--sym-pulse-close` - ❀ (Shutdown)
- `--sym-db` - ⊔ (Database)
- `--sym-prose` - ▣ (Documentation)

### Regeneration

**Do not edit manually.** These files are regenerated from Go source:

```bash
make types              # Regenerate all types including CSS
qntx typegen --lang css # Generate CSS only
```

Changes to `sym/symbols.go` will automatically propagate to CSS on next generation.
