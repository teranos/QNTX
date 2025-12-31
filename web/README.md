# QNTX Web UI

This directory contains the web interface for QNTX, including the ATS query editor with real-time graph visualization.

## Architecture

### Runtime Dependencies
- **No NPM required at runtime** - All JavaScript is bundled and embedded in the Go binary
- WebSocket for real-time updates (graph data and LSP)
- D3.js for graph visualization
- CodeMirror 6 for the ATS query editor with LSP integration

### Build System: Bun

This project uses **Bun** as a fast JavaScript bundler and package manager. Bun is chosen because:
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

### What Gets Built

**Input:** Everything in `web/` directory
- `index.html` - Main HTML template
- `js/` - JavaScript source files (imported from vendor bundles + custom code)
- `css/` - Stylesheets
- `fonts/` - Custom fonts
- `qntx.jpg` - Logo image

**Output:** `internal/server/dist/` (embedded in Go binary)
- `index.html` - Optimized HTML
- `js/main.js` - Bundled and minified JavaScript (all dependencies included)
- `css/` - CSS files
- `fonts/` - Font files
- `qntx.jpg` - Image asset

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
    "d3": "7.9.0"                             // Graph visualization
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
// 2. Bundle JavaScript with Bun's bundler
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

## File Structure

```
web/
├── package.json          # Exact dependency versions (sacred)
├── bun.lock              # Lock file (commit this)
├── build.ts              # Build script
├── index.html            # Main HTML template
├── qntx.jpg              # Logo image
├── css/
│   ├── core.css          # Core styles
│   ├── codemirror.css    # Editor styles
│   ├── graph.css         # Graph visualization styles
│   └── ...
├── js/
│   ├── main.js           # Entry point
│   ├── codemirror-editor.js       # CodeMirror setup
│   ├── ats-semantic-tokens-client.js  # Custom parse protocol
│   ├── websocket.js      # WebSocket manager
│   ├── graph-renderer.js # D3 graph visualization
│   └── vendor/           # Pre-built libraries (not bundled)
│       ├── d3.v7.min.js  # D3 (vendored, not npm)
│       └── codemirror-bundle.js   # Old - now replaced by bun bundle
└── README.md             # This file
```

## Key Features Enabled

### CodeMirror 6 with LSP
- Syntax highlighting (semantic tokens from LSP)
- Autocomplete (via LSP)
- Error/warning linting (via `@codemirror/lint`)
- Real-time editing with live graph updates

### Graph Visualization
- D3.js force-directed graph
- Interactive node filtering via legend
- Real-time updates as queries execute

### WebSocket Protocols

**Two protocols running on different endpoints:**

1. **LSP Protocol** (`/lsp`) - Standard Language Server Protocol
   - Completions, hover info, diagnostics
   - Handled by `internal/ats/lsp/`

2. **Custom Protocol** (`/ws`) - Graph updates, logs, custom parse tokens
   - Query responses (graph data)
   - Semantic tokens for syntax highlighting
   - Handled by `internal/server/handlers.go`

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

## Future Improvements

- [ ] Lazy-load non-critical UI components
- [ ] Service worker for offline support
- [ ] Progressive enhancement for slow networks
- [ ] Internationalization support
- [ ] Dark mode theme toggle
