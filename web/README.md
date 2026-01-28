# QNTX Web UI

Web interface for QNTX built on the [Glyph](../docs/vision/glyphs.md) primitive - a universal UI element that morphs between states while maintaining identity.

## Glyph System

Glyphs (⧉) are the atoms of the QNTX interface. A glyph is exactly ONE DOM element for its entire lifetime - it morphs between visual states (dot, window, canvas, modal) through smooth animations.

**Core glyph types:**
- `glyph.ts` - Base interface and constants
- `canvas-glyph.ts` - Spatial canvas with grid layout
- `py-glyph.ts` - Python editor with `attest()` support
- `ix-glyph.ts` - Ingest operations
- `result-glyph.ts` - Execution results

**Infrastructure:**
- `run.ts` - GlyphRun (the tray where collapsed glyphs live)
- `proximity.ts` - Proximity-based expansion
- `morph-transaction.ts` - Animation orchestration
- `manifestations/` - How glyphs render when expanded (window, canvas, etc.)

See [Glyphs Vision](../docs/vision/glyphs.md) for the full architectural vision including attestable glyph state.

## Why Web UI

The web UI is essential for QNTX's vision of Continuous Intelligence:
- **Glyph-based visualization** - entities manifest as glyphs that morph and persist state
- **Real-time updates** show intelligence evolving as new attestations arrive
- **Foundation for visions** documented in [tile-based semantic UI](../docs/vision/tile-based-semantic-ui.md) and [time-travel](../docs/vision/time-travel.md)

While `ax` queries provide the data, glyphs make the relationships tangible.

## Architecture

### Configuration
Ports are configured in `../am.toml` at project root:
- `[server].port` - Backend API port (default: 877)
- `[server].frontend_port` - Development server port (default: 8820)

### Runtime Dependencies
- **No NPM required at runtime** - All TypeScript is bundled and embedded in the Go binary
- WebSocket for real-time updates and LSP
- WAAPI (Web Animations API) for glyph morphing
- CodeMirror 6 for the ATS query editor with LSP integration

### Build System: Bun

This project uses **Bun** as a fast bundler and package manager. Bun is chosen because:
- **Fast builds** - 10x faster than npm/webpack
- **Integrated tooling** - Single tool for dependencies, bundling, and runtime
- **Reproducible builds** - Lock file ensures exact versions

## Development Workflow

### Prerequisites
- **Bun** (v1.3.3 or compatible) - [Install Bun](https://bun.sh)
- No Node.js required (Bun is a standalone runtime)

### Building the Bundle

The web assets are bundled into `internal/server/dist/` for embedding in the Go binary:

```bash
cd web
bun install              # Install dependencies (creates bun.lock)
bun run build            # Build bundle to internal/server/dist/
```

This is automatically called before `make cli` builds the Go binary.

### Understanding Dependencies

The `package.json` locks **exact versions** (no `^` or `~`):

```json
{
  "devDependencies": {
    "@codemirror/autocomplete": "6.18.3",    // Exact version
    "@codemirror/commands": "6.7.1",
    "@codemirror/language": "6.10.3",
    "@codemirror/lint": "6.8.2",             // Error/warning linting
    "@codemirror/state": "6.4.1",
    "@codemirror/view": "6.34.3",
    "@lezer/highlight": "1.2.1",
    "codemirror-languageserver": "1.17.0",   // LSP client
    "d3": "7.9.0"                             // Legacy graph (being replaced by glyphs)
  }
}
```

**Why exact versions?**
- Infrastructure is sacred - every version change is a build change
- Reproducible builds across all developers and CI/CD
- When upgrading, it's deliberate and documented

### Adding or Updating Dependencies

**If you need a new dependency:**

1. **Identify the exact version** you need (check npm.org or documentation)
2. **Update package.json** with the exact version (no `^` or `~`)
3. **Rebuild:**
   ```bash
   bun install    # Updates bun.lock
   bun run build  # Rebuilds bundle
   ```
4. **Test the bundle works** before committing
5. **Document why** in your commit message

**Example:** Adding a new utility library
```json
{
  "devDependencies": {
    "some-lib": "1.2.3"  // Exact version, not "^1.2.3"
  }
}
```

### The Build Process (build.ts)

`build.ts` orchestrates the bundling:

```typescript
// 1. Clean dist/ directory
// 2. Bundle with Bun's bundler
//    - Resolves all imports
//    - Includes dependencies from package.json
//    - Minifies output
// 3. Copy HTML, CSS, fonts, assets
```

### Testing the Bundle

**After rebuilding, always test:**

```bash
# Rebuild Go binary (includes new bundle)
make cli

# Start server in test mode
./bin/qntx server --test-mode

# Or without test mode (requires existing database)
./bin/qntx server

# Visit http://localhost:877 in browser
# Verify:
# - Page loads
# - CodeMirror editor appears
# - Graph visualization loads
# - Search/query works
```

## Key Features Enabled

### Glyph System
- Proximity-based morphing (dot → expanded → window)
- WAAPI animations for smooth state transitions
- Canvas glyph with spatial grid layout
- Python programmature glyphs with `attest()` support

### CodeMirror 6 with LSP
- Syntax highlighting (semantic tokens from LSP)
- Autocomplete (via LSP)
- Error/warning linting (via `@codemirror/lint`)
- Real-time editing with live updates

## Troubleshooting

### "command not found: bun"
Install Bun: https://bun.sh

### Bundle too large?
Check `js/main.js` size after build. If larger than expected:
- Verify dependencies in package.json are necessary
- Check for duplicate imports across files
- Consider lazy-loading less critical features

### Changes not appearing in app
1. Did you run `bun run build`? (Creates dist/)
2. Did you run `make cli`? (Rebuilds Go binary with new dist/)
3. Are you running the new binary? (`./bin/qntx`)

### Outdated versions causing issues
Check `bun.lock` - if versions don't match package.json:
```bash
bun install  # Resync package.json and bun.lock
```

## Testing

### Running Tests
```bash
bun test              # Fast tests only (~1.5s)
USE_JSDOM=1 bun test  # All tests including DOM tests (~9s)
```

### Writing DOM Tests
DOM tests using JSDOM are slow. Gate them behind `USE_JSDOM=1` so they only run in CI:

```typescript
// At top of *.dom.test.ts file
const USE_JSDOM = process.env.USE_JSDOM === '1';

describe('YourComponent', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }
    // ... your tests
});
```

See `ts/vidstream-window.dom.test.ts` for example.
