# Canvas Export - Server-Side Rendering via TypeScript Plugin

## Documentation Review

**ADRs Read:**
- [ADR-001: Domain Plugin Architecture](../adr/ADR-001-domain-plugin-architecture.md)
- [ADR-002: Plugin Configuration Management](../adr/ADR-002-plugin-configuration.md) ✅ **CRITICAL**
- [ADR-003: Plugin Communication Patterns](../adr/ADR-003-plugin-communication.md)

**Guides Read:**
- [TypeScript Plugin Support](../development/ts-plugin.md) - Reference implementation plan
- [External Plugin Guide](../development/external-plugin-guide.md) - gRPC protocol details

## Context

QNTX needs to render canvas snapshots server-side for:
1. Automated Bluesky posts with IPFS links
2. Static canvas publishing
3. Sharing canvas snapshots without client interaction

**Current State:**
- Client-side DOM export exists (`/api/canvas/export-dom`)
- Canvas-building code is in `web/ts/components/glyph/canvas/`
- No TypeScript plugin runtime exists yet

**Goal:** Run the same browser TypeScript code server-side using Bun subprocess as a gRPC plugin.

## Critical Learning from Attempt 3

**What went wrong:**
- Did not read ADR-002 before starting
- Built plugin in non-standard location (`plugin/typescript/examples/canvas-renderer/`)
- Never verified plugin could be discovered by the system
- Jumped to Phase 2/3 without validating Phase 1

**ADR-002 Requirements (MUST FOLLOW):**
1. Plugins must be in configured search paths: `~/.qntx/plugins` or `./plugins`
2. Binary names must follow conventions: `qntx-{name}-plugin`, `qntx-{name}`, or `{name}`
3. Binaries must be executable or `.ts` files
4. Enabled in `am.toml`: `[plugin].enabled = ["canvas-renderer"]`
5. Discovery happens via `LoadPluginsFromConfig()` → `discoverPlugin()` → binary search

## Three-Phase Plan

---

## Phase 1: TypeScript Plugin Runtime with "Hello World" (~1 day)

**Goal:** Verify end-to-end that a TypeScript plugin can be discovered, launched via Bun, and respond to gRPC requests.

### Why This Phase Matters

This establishes TypeScript as a supported plugin language. Without this working, nothing else matters.

### 1.1 Create TypeScript Runtime Infrastructure

**Location:** `plugin/typescript/runtime/`

**Files to create:**
```
plugin/typescript/runtime/
├── package.json         # Bun dependencies
├── tsconfig.json        # TypeScript config
├── main.ts              # Entry point - starts gRPC server
└── plugin-service.ts    # Implements DomainPluginService
```

**Dependencies (package.json):**
```json
{
  "name": "@qntx/typescript-runtime",
  "type": "module",
  "dependencies": {
    "@grpc/grpc-js": "^1.10.0",
    "@grpc/proto-loader": "^0.7.10"
  }
}
```

**Key functionality:**
- Load plugin module from path (passed via `--plugin-path` CLI arg)
- Start gRPC server on port (passed via `--grpc-port` or auto-allocate)
- Print `GRPC_ADDRESS=host:port` to stdout for Go discovery
- Implement all DomainPluginService methods (Init, Metadata, RegisterHTTP, etc.)

### 1.2 Create Hello World Test Plugin

**Location:** `./qntx-plugins/hello-world/plugin.ts`

Why `./qntx-plugins/` and not `plugin/typescript/examples/`? **Because ADR-002 specifies `./qntx-plugins` as a search path for QNTX-maintained plugins.**

**File structure:**
```
./plugins/hello-world/
├── plugin.ts        # Main plugin file
└── package.json     # Mark as QNTX plugin: {"qntx-plugin": true}
```

**plugin.ts minimal implementation:**
```typescript
export default {
    name: 'hello-world',
    version: '1.0.0',

    async init(config: any) {
        console.log('[HelloWorld] Initialized');
        return { success: true };
    },

    registerHTTP(mux: any) {
        mux.handle('GET', '/hello', (req: any, res: any) => {
            res.json({ message: 'Hello from TypeScript!' });
        });
    }
}
```

### 1.3 Update Go Discovery to Support TypeScript

**Files to modify:**
- `plugin/grpc/discovery.go` - Already has TypeScript detection (line 336-338)
- `plugin/grpc/loader.go` - Already has `launchPlugin` that wraps Bun (line 314-436)

**Verify existing code:**
```go
// discovery.go line 336-338
isTypeScriptPlugin := strings.HasSuffix(binary, ".ts") || isPackageJSONPlugin(binary)
```

This should already work! But we need to verify the search finds it.

### 1.4 Configuration

**Project am.toml:**
```toml
[plugin]
enabled = ["hello-world"]
paths = ["~/.qntx/plugins", "./plugins"]  # Default, but explicit
```

**Plugin placement options:**
1. `./plugins/hello-world/plugin.ts` (preferred - project-level)
2. `./plugins/hello-world` (if package.json has "qntx-plugin": true)
3. `~/.qntx/plugins/qntx-hello-world` (user-level, requires symlink)

### 1.5 Verification Steps (CRITICAL - DO NOT SKIP)

**Step 1: Manual runtime test**
```bash
cd plugin/typescript/runtime
bun install
bun run main.ts --plugin-path ../../plugins/hello-world/plugin.ts --grpc-port 50051
```

Expected output:
```
GRPC_ADDRESS=127.0.0.1:50051
[HelloWorld] Runtime started
```

**Step 2: gRPC connectivity test**
```bash
# In another terminal
grpcurl -plaintext localhost:50051 qntx.plugin.DomainPluginService/Metadata
```

Expected: Returns plugin metadata JSON

**Step 3: Discovery test**
```bash
# Add to am.toml: enabled = ["hello-world"]
# Start server
make dev
```

Expected in logs:
```
plugin-loader  Searching for 'hello-world' plugin binary in 2 paths
plugin-loader  Will load 'hello-world' plugin from binary: ./plugins/hello-world/plugin.ts
plugin-loader  Launching TypeScript plugin via Bun runtime
hello-world    GRPC_ADDRESS=127.0.0.1:38700
plugin-loader  Plugin 'hello-world' v1.0.0 loaded and ready
```

**Step 4: HTTP handler test**
```bash
curl http://localhost:8772/api/hello-world/hello
```

Expected: `{"message": "Hello from TypeScript!"}`

### Phase 1 Success Criteria

- [ ] Bun runtime starts and prints GRPC_ADDRESS
- [ ] grpcurl can call Metadata() and get response
- [ ] Go discovery finds `./plugins/hello-world/plugin.ts`
- [ ] Plugin appears in server logs as loaded
- [ ] HTTP request to `/api/hello-world/hello` returns JSON
- [ ] `make test` passes (if we add tests)

**DO NOT PROCEED TO PHASE 2 UNTIL ALL CRITERIA ARE MET**

---

## Phase 2: Canvas Renderer Plugin (~2 days)

**Goal:** First production TypeScript plugin that renders canvas HTML using shared browser code.

### Prerequisites

- [ ] Phase 1 fully working and verified
- [ ] Hello world plugin loads and responds
- [ ] Understand Bun vs Node.js (use Bun, not Node!)

### 2.1 Choose DOM Library

**Options:**
- jsdom (ts-plugin.md recommends this)
- happy-dom (lighter weight, Bun-friendly)

**Decision:** Start with happy-dom, fallback to jsdom if issues.

**Rationale:** happy-dom is lighter and Bun already uses it internally. jsdom is more mature but heavier.

### 2.2 Create Shared Canvas Builder

**Problem:** Current canvas-building code assumes browser environment.

**Solution:** Make it environment-agnostic by:
1. Accepting `document` as parameter (no `window.document` access)
2. Avoiding browser-only APIs (no `addEventListener` in builder)
3. Exporting pure rendering functions

**Files to create/modify:**
```
web/ts/components/glyph/canvas/
├── canvas-workspace-builder.ts  # Modify to accept document param
└── canvas-builder-shared.ts     # New: environment-agnostic wrapper
```

**Key change:**
```typescript
// Before (browser-only)
export function buildCanvasWorkspace(canvasId: string, glyphs: Glyph[]): HTMLElement {
    const workspace = document.createElement('div');  // ❌ Assumes global
    // ...
}

// After (environment-agnostic)
export function buildCanvasWorkspace(
    canvasId: string,
    glyphs: Glyph[],
    document: Document  // ✅ Injected
): HTMLElement {
    const workspace = document.createElement('div');
    // ...
}
```

### 2.3 Handle Browser API Dependencies

**Challenge:** Code may use:
- `window.getComputedStyle()`
- `element.getBoundingClientRect()`
- Event listeners
- CSS selectors

**Approach:**
1. Identify browser API usage in canvas-building code
2. Either:
   - Remove if not needed for static rendering
   - Stub out if needed (e.g., `getBoundingClientRect` returns fixed dimensions)
   - Use environment flag: `if (typeof window !== 'undefined')`

**Create:** `plugin/typescript/runtime/browser-stubs.ts`

```typescript
// Minimal stubs for server-side rendering
export function setupBrowserStubs(globalThis: any) {
    if (typeof globalThis.window === 'undefined') {
        // Stub window.getComputedStyle
        globalThis.window = {
            getComputedStyle: () => ({
                getPropertyValue: () => ''
            })
        };
    }
}
```

### 2.4 Build Canvas Renderer Plugin

**Location:** `./plugins/canvas-renderer/`

**Structure:**
```
./plugins/canvas-renderer/
├── plugin.ts           # Main plugin
├── dom-env.ts          # happy-dom setup
├── css-loader.ts       # Load CSS files
├── package.json        # {"qntx-plugin": true}
└── README.md           # Plugin documentation
```

**plugin.ts core logic:**
```typescript
import { Window } from 'happy-dom';
import { buildCanvasWorkspace } from '../../../web/ts/components/glyph/canvas/canvas-workspace-builder';

export default {
    name: 'canvas-renderer',
    version: '1.0.0',

    async init(config: any) {
        console.log('[CanvasRenderer] Initialized');
        return { success: true };
    },

    registerHTTP(mux: any) {
        mux.handle('POST', '/render', async (req: any, res: any) => {
            const { canvas_id, glyphs } = await req.json();

            // Create server-side DOM
            const window = new Window();
            const document = window.document;

            // Build canvas using shared code
            const workspace = buildCanvasWorkspace(canvas_id, glyphs, document);

            // Load CSS
            const css = loadCanvasCSS();

            // Build HTML
            const html = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>${css}</style>
</head>
<body>${workspace.outerHTML}</body>
</html>`;

            res.json({ html });
        });
    }
}
```

### 2.5 CSS Bundling

**Challenge:** Canvas needs CSS from multiple files.

**Approach:**
```typescript
// css-loader.ts
import { readFileSync } from 'fs';
import { join } from 'path';

const CSS_FILES = [
    'web/css/core.css',
    'web/css/canvas.css',
    'web/css/glyph.css',
];

export function loadCanvasCSS(): string {
    const root = process.cwd();  // QNTX repo root
    return CSS_FILES.map(file => {
        const path = join(root, file);
        return readFileSync(path, 'utf-8');
    }).join('\n');
}
```

### 2.6 Configuration

**Project am.toml:**
```toml
[plugin]
enabled = ["canvas-renderer"]  # Remove hello-world if no longer needed
```

### 2.7 Verification Steps

**Step 1: Plugin loads**
```bash
make dev
# Check logs for "canvas-renderer v1.0.0 loaded"
```

**Step 2: Render endpoint works**
```bash
curl -X POST http://localhost:8772/api/canvas-renderer/render \
  -H 'Content-Type: application/json' \
  -d '{
    "canvas_id": "test",
    "glyphs": [{
      "id": "note-1",
      "symbol": "▣",
      "x": 100,
      "y": 100,
      "width": 200,
      "height": 150,
      "content": "Test note"
    }]
  }'
```

Expected: Returns `{"html": "<!DOCTYPE html>..."}`

**Step 3: HTML structure validation**
```bash
# Save output to file
curl ... > test-output.html

# Verify structure
grep 'canvas-workspace' test-output.html
grep 'data-canvas-id="test"' test-output.html
grep 'Test note' test-output.html
```

**Step 4: Visual comparison**
- Open `test-output.html` in browser
- Create same canvas in QNTX client
- Compare visually

### Phase 2 Success Criteria

- [ ] canvas-renderer plugin loads successfully
- [ ] POST `/api/canvas-renderer/render` returns HTML
- [ ] HTML contains correct DOM structure
- [ ] CSS is included and renders correctly
- [ ] Visual output matches client rendering
- [ ] No console errors in plugin logs
- [ ] `make test` passes

**DO NOT PROCEED TO PHASE 3 UNTIL ALL CRITERIA ARE MET**

---

## Phase 3: Production Integration (~1 day)

**Goal:** Wire canvas-renderer into QNTX export/publish flows.

### Prerequisites

- [ ] Phase 2 fully working and verified
- [ ] canvas-renderer plugin renders correctly
- [ ] Visual output validated

### 3.1 Add Go Export Handler

**File:** `glyph/handlers/canvas_export.go`

Add new method:
```go
func (h *CanvasHandler) HandleExport(w http.ResponseWriter, r *http.Request) {
    canvasID := r.URL.Query().Get("canvas_id")
    if canvasID == "" {
        http.Error(w, "canvas_id required", http.StatusBadRequest)
        return
    }

    // Get glyphs from storage
    glyphs, err := h.store.ListGlyphsByCanvas(r.Context(), canvasID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Get canvas-renderer plugin
    manager := grpcplugin.GetDefaultPluginManager()
    plugin, ok := manager.GetPlugin("canvas-renderer")
    if !ok {
        http.Error(w, "canvas-renderer plugin not loaded", http.StatusServiceUnavailable)
        return
    }

    // Call plugin via gRPC
    // TODO: Implement plugin.CallHTTP or similar

    // Write HTML to docs/demo/index.html
    // TODO: Write response
}
```

**Route:** `server/routing.go`
```go
http.HandleFunc("/api/canvas/export", wrap(s.canvasHandler.HandleExport))
```

### 3.2 Add Frontend API

**File:** `web/ts/api/canvas.ts`

```typescript
export async function exportCanvas(canvasId: string): Promise<void> {
    const response = await apiFetch('/api/canvas/export', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ canvas_id: canvasId }),
    });

    if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Export failed');
    }

    const result = await response.json();
    log.info(SEG.GLYPH, `Canvas exported to ${result.path}`);
}
```

### 3.3 Update Export Button

**File:** `web/ts/components/glyph/manifestations/canvas-expanded.ts`

Find the export button (currently calls `exportCanvasDOM`):
```typescript
// Replace exportCanvasDOM with exportCanvas
import { exportCanvas } from '../../../api/canvas';

const exportBtn = new Button({
    label: 'Export',
    icon: '↓',
    onClick: async () => {
        await exportCanvas(glyph.id);  // Pass canvas ID, not workspace element
        log.info(SEG.GLYPH, '[Canvas] Export complete (server-side)');
    }
});
```

### 3.4 Verification Steps

**Step 1: End-to-end export flow**
```bash
make demo
# 1. Create a subcanvas
# 2. Double-click to expand fullscreen
# 3. Add some glyphs (note, code, etc.)
# 4. Click Export button
# 5. Check docs/demo/index.html exists
```

**Step 2: HTML output validation**
```bash
# Open docs/demo/index.html in browser
# Verify:
# - Canvas renders correctly
# - All glyphs visible
# - CSS applied
# - No console errors
```

**Step 3: Compare client vs server rendering**
- Export same canvas via old client method
- Export via new server method
- Compare HTML structure (minor differences OK, visual should match)

### Phase 3 Success Criteria

- [ ] Export button in canvas-expanded calls server endpoint
- [ ] Server fetches glyphs from database
- [ ] Server calls canvas-renderer plugin via gRPC
- [ ] HTML written to docs/demo/index.html
- [ ] Opening HTML file shows correct canvas
- [ ] Visual output matches client rendering
- [ ] `make test` passes
- [ ] `make demo` works end-to-end

---

## Testing Strategy

### Unit Tests

**TypeScript (Bun):**
- `plugin/typescript/runtime/*.test.ts` - Runtime gRPC service
- `./plugins/canvas-renderer/*.test.ts` - Canvas rendering logic

**Go:**
- `plugin/grpc/loader_test.go` - Add TypeScript plugin discovery tests
- `glyph/handlers/canvas_export_test.go` - Export handler tests

### Integration Tests

**Phase 1:**
- Go server → TypeScript plugin gRPC communication
- Plugin discovery and launch
- HTTP handler registration

**Phase 2:**
- Canvas rendering with happy-dom
- CSS loading and injection
- HTML structure validation

**Phase 3:**
- End-to-end export flow
- Database → Plugin → HTML file

### Manual Testing Checklist

**Phase 1:**
- [ ] Runtime starts manually
- [ ] grpcurl can query plugin
- [ ] Discovery finds plugin
- [ ] HTTP endpoint responds

**Phase 2:**
- [ ] Plugin renders simple canvas
- [ ] Multiple glyph types work
- [ ] CSS applies correctly
- [ ] HTML validates

**Phase 3:**
- [ ] Export button works
- [ ] HTML file created
- [ ] Browser renders correctly
- [ ] Multiple exports work

---

## Dependencies

**Required:**
- Bun runtime: `curl -fsSL https://bun.sh/install | bash`
- Protocol buffers: Already in project
- happy-dom: `bun add happy-dom` (in plugin directory)

**Verification:**
```bash
bun --version  # Should be 1.0+
which protoc   # Should exist
```

---

## Rollback Plan

If any phase fails and cannot be fixed quickly:

**Phase 1 failure:** TypeScript plugin infrastructure doesn't work
- Rollback: Remove TypeScript runtime code
- Fallback: Stick with client-side export only
- Impact: No server-side rendering

**Phase 2 failure:** Canvas rendering doesn't work
- Rollback: Keep hello-world plugin, remove canvas-renderer
- Fallback: Client-side export remains
- Impact: TypeScript plugin infrastructure proven but no canvas use case

**Phase 3 failure:** Integration breaks
- Rollback: Keep plugin but don't wire to frontend
- Fallback: Plugin works, manual API calls only
- Impact: No UI button, API available for future use

---

## Success Metrics

**Phase 1:**
- TypeScript plugin loads and responds to gRPC
- Discovery system finds and launches it
- HTTP handlers work

**Phase 2:**
- Canvas HTML output matches client rendering
- All glyph types render correctly
- CSS applies properly

**Phase 3:**
- Export button uses server rendering
- HTML file written successfully
- Visual output validated

---

## Documentation to Create

- [ ] `plugin/typescript/README.md` - TypeScript runtime overview
- [ ] `./plugins/canvas-renderer/README.md` - Canvas renderer plugin docs
- [x] Update `docs/development/ts-plugin.md` - Add actual implementation notes
- [ ] `CANVAS-EXPORT-COMPLETE.md` - Final status (only after Phase 3 success)

---

## Critical Reminders

1. **Read ADR-002 requirements FIRST** ✅
2. **Verify each phase before proceeding** ✅
3. **Use `./qntx-plugins/` directory for QNTX-maintained plugins** ✅
4. **Follow naming conventions:** `qntx-{name}-plugin`, `qntx-{name}`, or `{name}` ✅
5. **Test discovery independently before integration** ✅
6. **We're using Bun, not Node.js** ✅
7. **Manual verification at each step** ✅
8. **Don't skip success criteria** ✅

---

## Phase 3 Status (2026-02-26)

**Completed:**
- ✅ Go export handler (`HandleExportStatic` in canvas.go)
- ✅ Frontend API (`exportCanvasStatic` in canvas.ts)
- ✅ Export button integration (canvas-expanded.ts)
- ✅ DEMO flag gating (export only available with `make demo`)
- ✅ canvas_id scoping (filters glyphs by subcanvas)
- ✅ Error display via Button component (no alert/confirm/prompt)
- ✅ Process improvements: ESLint rules, web/CLAUDE.md documentation, PreToolUse hooks
- ✅ Sync fix: canvas-sync.ts uses spread operator (all proto fields auto-sync)
- ✅ Test coverage: canvas_export_test.go validates core flow
- ✅ Tests pass: 666 pass, 0 fail

**Known Limitations (documented in canvas.go):**
- Old glyphs (created before 2026-02-26) have empty canvas_id and won't export
- Export quality issues: happy-dom rendering has limitations vs live browser

**Won't Do:**
- Migration script to backfill old glyphs (not worth the effort, new glyphs work)

**Out of Scope (future work):**
- Publish endpoint scoping (similar to export)
- Publish button in breadcrumb bar
- Export quality improvements

**Status:** Phase 3 functional, ready for review as POC
