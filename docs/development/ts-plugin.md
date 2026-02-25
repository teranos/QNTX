# TypeScript Plugin Support

**Goal:** Enable TypeScript as a third plugin language alongside Go and Python, allowing shared code between client and server for canvas rendering and other use cases.

## Motivation

QNTX needs to render canvas snapshots server-side for automated Bluesky posts and IPFS publishing. Rather than duplicating canvas-building logic in Go (fragile, diverges), we can run the same TypeScript code in both browser and server via a TypeScript plugin runtime.

**Pattern:** Same as Next.js/Remix SSR - one codebase, multiple runtimes.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│ QNTX Server (Go)                                    │
│                                                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │ Go Plugin│  │ Py Plugin│  │ TypeScript Plugin│ │
│  │          │  │          │  │  (Bun subprocess)│ │
│  └────┬─────┘  └────┬─────┘  └────────┬─────────┘ │
│       │             │                  │           │
│       └─────────────┴──────────────────┘           │
│                     │                              │
│              gRPC Plugin Protocol                  │
└─────────────────────────────────────────────────────┘
```

**Key insight:** TypeScript plugin imports the same canvas-building code that runs in the browser. No duplication.

## Three-Phase Plan

---

## Phase 1: TypeScript Plugin Runtime (~1-2 days)

**Goal:** Bun subprocess that speaks gRPC plugin protocol. Can register HTTP handlers and respond to requests.

### 1.1 Set up TypeScript plugin workspace

**Create:**
```
plugin/typescript/runtime/
├── package.json
├── tsconfig.json
├── main.ts              # Entry point, starts gRPC server
├── plugin-service.ts    # Implements plugin protocol methods
└── generated/           # Generated gRPC stubs
```

**Dependencies:**
```json
{
  "dependencies": {
    "@grpc/grpc-js": "^1.10.0",
    "@grpc/proto-loader": "^0.7.10"
  },
  "devDependencies": {
    "@types/node": "^20.0.0"
  }
}
```

### 1.2 Generate TypeScript gRPC stubs

```bash
# From plugin/grpc/protocol/*.proto
protoc --plugin=protoc-gen-ts_proto=./node_modules/.bin/protoc-gen-ts_proto \
  --ts_proto_out=./generated \
  plugin/grpc/protocol/*.proto
```

### 1.3 Implement core plugin service

**File:** `plugin-service.ts`

```typescript
import { Server, ServerCredentials } from '@grpc/grpc-js';
import { PluginService } from './generated/plugin_grpc_pb';

interface PluginModule {
    name: string;
    init: (config: InitRequest) => Promise<InitResponse>;
    registerHTTP: (mux: HttpMux) => HttpHandlers;
    registerWebSocket?: () => WebSocketHandlers;
}

class TypeScriptPluginService {
    private plugin: PluginModule;

    async Init(call, callback) {
        const config = call.request;
        const response = await this.plugin.init(config);
        callback(null, response);
    }

    async RegisterHTTP(call, callback) {
        const mux = createHttpMux();
        const handlers = this.plugin.registerHTTP(mux);
        callback(null, handlers);
    }

    // ... RegisterWebSocket, etc.
}
```

### 1.4 Create "hello world" test plugin

**Create:** `plugin/typescript/examples/hello-world/plugin.ts`

```typescript
export default {
    name: 'hello-world',

    init: async (config) => {
        console.log('[HelloWorld] Plugin initialized');
        return { success: true };
    },

    registerHTTP: (mux) => {
        mux.handle('/hello', (req, res) => {
            res.json({ message: 'Hello from TypeScript plugin!' });
        });

        return mux.getHandlers();
    }
}
```

### 1.5 Go integration - detect and start TS plugins

**Modify:** `plugin/grpc/discovery.go`

```go
func detectPluginType(path string) string {
    // TypeScript plugin detection
    if strings.HasSuffix(path, ".ts") {
        return "typescript"
    }

    // Check for package.json with "qntx-plugin" field
    if hasPackageJSON(path) {
        pkgData := readPackageJSON(path)
        if pkgData.QNTXPlugin {
            return "typescript"
        }
    }

    // Existing Go/Python detection
    // ...
}

func hasPackageJSON(dir string) bool {
    _, err := os.Stat(filepath.Join(dir, "package.json"))
    return err == nil
}
```

**Modify:** `plugin/grpc/server.go`

```go
func (r *PluginRegistry) startTypeScriptPlugin(meta PluginMetadata) error {
    // Start Bun subprocess with TypeScript runtime
    cmd := exec.Command("bun", "run",
        filepath.Join("plugin/typescript/runtime/main.ts"),
        "--plugin-path", meta.Path,
        "--grpc-port", "0") // Runtime picks port and prints to stdout

    // Capture stdout to get gRPC address
    stdout, err := cmd.StdoutPipe()
    if err != nil {
        return errors.Wrap(err, "failed to create stdout pipe")
    }

    if err := cmd.Start(); err != nil {
        return errors.Wrap(err, "failed to start typescript runtime")
    }

    // Parse stdout for gRPC address (e.g., "GRPC_ADDRESS=localhost:12345")
    scanner := bufio.NewScanner(stdout)
    var grpcAddr string
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "GRPC_ADDRESS=") {
            grpcAddr = strings.TrimPrefix(line, "GRPC_ADDRESS=")
            break
        }
    }

    // Connect to plugin via gRPC
    conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
    if err != nil {
        cmd.Process.Kill()
        return errors.Wrapf(err, "failed to connect to typescript plugin at %s", grpcAddr)
    }

    // Store in registry
    r.plugins.Store(meta.Name, &Plugin{
        Metadata: meta,
        Conn: conn,
        Process: cmd.Process,
    })

    return nil
}
```

### Phase 1 Success Criteria

- [ ] Bun runtime starts and accepts gRPC connections
- [ ] Can call `Init()` and get response
- [ ] Can register HTTP handler via `RegisterHTTP()`
- [ ] Go server detects TS plugin and starts it
- [ ] HTTP request to `/api/hello-world/hello` returns JSON
- [ ] Plugin logs appear in server output

---

## Phase 2: Canvas Renderer Plugin (~2-3 days)

**Goal:** First real TypeScript plugin that renders canvas HTML using shared TS code.

### 2.1 Extract shared canvas code

Make canvas-building code importable by both client and plugin.

**Create:** `web/ts/shared/canvas-builder.ts`

```typescript
/**
 * Environment-agnostic canvas workspace builder.
 * Works in both browser and Node.js (with jsdom).
 */
export interface CanvasBuilderOptions {
    canvasId: string;
    glyphs: Glyph[];
    document: Document; // Injected - browser or jsdom
}

export function buildCanvasWorkspace(options: CanvasBuilderOptions): HTMLElement {
    const { canvasId, glyphs, document } = options;

    // Create workspace container
    const workspace = document.createElement('div');
    workspace.className = 'canvas-workspace';
    workspace.setAttribute('data-canvas-id', canvasId);
    workspace.tabIndex = 0;

    // Create content layer
    const contentLayer = document.createElement('div');
    contentLayer.className = 'canvas-content-layer';

    // Render glyphs
    for (const glyph of glyphs) {
        const element = createGlyphElement(glyph, document);
        contentLayer.appendChild(element);
    }

    workspace.appendChild(contentLayer);
    return workspace;
}

function createGlyphElement(glyph: Glyph, document: Document): HTMLElement {
    // Glyph rendering logic (no browser globals)
    // ...
}
```

**Modify:** `web/ts/components/glyph/canvas/canvas-workspace-builder.ts`

```typescript
// Use shared builder
import { buildCanvasWorkspace } from '../../../shared/canvas-builder';

export function buildCanvasWorkspaceInBrowser(canvasId: string, glyphs: Glyph[]): HTMLElement {
    return buildCanvasWorkspace({
        canvasId,
        glyphs,
        document: window.document // Browser document
    });
}
```

### 2.2 Set up server-side DOM environment

**Create:** `plugin/typescript/runtime/dom-env.ts`

```typescript
import { JSDOM } from 'jsdom';

export interface DOMEnvironment {
    document: Document;
    window: Window;
}

export function createDOMEnvironment(): DOMEnvironment {
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost',
        pretendToBeVisual: true,
        runScripts: 'outside-only',
    });

    return {
        document: dom.window.document,
        window: dom.window as any,
    };
}
```

**Add dependencies:**

```json
{
  "dependencies": {
    "jsdom": "^24.0.0",
    "@grpc/grpc-js": "^1.10.0"
  }
}
```

### 2.3 Build canvas renderer plugin

**Create:** `qntx-canvas-renderer/plugin.ts`

```typescript
import { buildCanvasWorkspace } from '../web/ts/shared/canvas-builder';
import { createDOMEnvironment } from '../plugin/typescript/runtime/dom-env';
import { loadCanvasCSS } from './css-loader';

export default {
    name: 'canvas-renderer',

    init: async (config) => {
        console.log('[CanvasRenderer] Plugin initialized');
        return { success: true };
    },

    registerHTTP: (mux) => {
        // POST /render - Render canvas to HTML
        mux.handle('POST', '/render', async (req, res) => {
            const { canvasId, glyphs } = await req.json();

            // Create server-side DOM
            const { document } = createDOMEnvironment();

            // Build canvas using shared code
            const workspace = buildCanvasWorkspace({
                canvasId,
                glyphs,
                document
            });

            // Load CSS
            const css = loadCanvasCSS();

            // Build complete HTML
            const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, user-scalable=no">
<title>QNTX Canvas - ${canvasId}</title>
<style>
${css}
</style>
</head>
<body>
${workspace.outerHTML}
</body>
</html>`;

            res.json({ html });
        });

        return mux.getHandlers();
    }
}
```

### 2.4 CSS handling

**Create:** `qntx-canvas-renderer/css-loader.ts`

```typescript
import fs from 'fs';
import path from 'path';

const CSS_FILES = [
    'web/css/core.css',
    'web/css/canvas.css',
    'web/css/components.css',
    'web/css/glyph.css',
];

export function loadCanvasCSS(): string {
    const rootDir = path.resolve(__dirname, '../..');

    return CSS_FILES.map(file => {
        const filePath = path.join(rootDir, file);
        return fs.readFileSync(filePath, 'utf-8');
    }).join('\n');
}
```

### 2.5 Testing

**Create:** `qntx-canvas-renderer/test.ts`

```typescript
import { test, expect } from 'bun:test';

test('renders canvas with note glyph', async () => {
    const glyphs = [{
        id: 'note-1',
        symbol: '▣',
        x: 100,
        y: 100,
        width: 200,
        height: 150,
        content: 'Test note'
    }];

    const response = await fetch('http://localhost:877/api/canvas-renderer/render', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ canvasId: 'test', glyphs })
    });

    const { html } = await response.json();

    // Verify structure
    expect(html).toContain('<!DOCTYPE html>');
    expect(html).toContain('canvas-workspace');
    expect(html).toContain('data-canvas-id="test"');
    expect(html).toContain('canvas-content-layer');

    // Verify glyph content
    expect(html).toContain('Test note');
    expect(html).toContain('data-glyph-id="note-1"');

    // Verify CSS included
    expect(html).toContain('.canvas-workspace');
});

test('output matches client-rendered HTML', async () => {
    // Compare server-rendered vs client-rendered
    // (Manual verification initially)
});
```

### Phase 2 Success Criteria

- [ ] Canvas renderer plugin starts successfully
- [ ] Can call `/api/canvas-renderer/render` with glyphs
- [ ] Returns HTML that matches client-rendered output
- [ ] Includes all necessary CSS
- [ ] DOM structure is correct (workspace, content-layer, glyphs)
- [ ] Tests pass

---

## Phase 3: Integration & Bluesky Use Case (~1-2 days)

**Goal:** Use canvas renderer plugin for automated Bluesky posts with canvas snapshots.

### 3.1 Create canvas snapshot endpoint

**Create:** `server/canvas_snapshot.go`

```go
package server

import (
    "encoding/json"
    "net/http"

    "github.com/teranos/QNTX/errors"
)

// HandleCanvasSnapshot renders a canvas to HTML via TypeScript plugin
func (s *QNTXServer) HandleCanvasSnapshot(w http.ResponseWriter, r *http.Request) {
    canvasID := r.URL.Query().Get("canvas_id")
    if canvasID == "" {
        s.writeError(w, errors.New("canvas_id is required"), http.StatusBadRequest)
        return
    }

    // Get glyphs from storage
    glyphs, err := s.canvasHandler.store.ListGlyphs(r.Context())
    if err != nil {
        s.writeError(w, errors.Wrap(err, "failed to list glyphs"), http.StatusInternalServerError)
        return
    }

    // Filter glyphs for this canvas
    var canvasGlyphs []interface{}
    for _, g := range glyphs {
        if g.CanvasID == canvasID {
            canvasGlyphs = append(canvasGlyphs, map[string]interface{}{
                "id": g.ID,
                "symbol": g.Symbol,
                "x": g.X,
                "y": g.Y,
                "width": g.Width,
                "height": g.Height,
                "content": g.Content,
            })
        }
    }

    // Call TypeScript plugin
    plugin, ok := s.pluginRegistry.Get("canvas-renderer")
    if !ok {
        s.writeError(w, errors.New("canvas-renderer plugin not found"), http.StatusServiceUnavailable)
        return
    }

    resp, err := plugin.CallHTTP("/render", map[string]interface{}{
        "canvasId": canvasID,
        "glyphs": canvasGlyphs,
    })
    if err != nil {
        s.writeError(w, errors.Wrap(err, "plugin render failed"), http.StatusInternalServerError)
        return
    }

    html := resp["html"].(string)

    // Optional: Pin to IPFS
    var ipfsCID string
    if s.ipfsEnabled() {
        cid, err := s.pinToIPFS([]byte(html))
        if err != nil {
            s.logger.Warnw("Failed to pin canvas to IPFS", "error", err)
        } else {
            ipfsCID = cid
        }
    }

    writeJSON(w, map[string]string{
        "html": html,
        "ipfs_cid": ipfsCID,
        "url": fmt.Sprintf("https://ipfs.io/ipfs/%s", ipfsCID),
    })
}
```

**Modify:** `server/routing.go`

```go
http.HandleFunc("/api/canvas/snapshot", wrap(s.HandleCanvasSnapshot))
```

### 3.2 Integrate with Bluesky posting flow

**Modify:** `qntx-atproto/plugin.go`

```go
func (p *AtprotoPlugin) postWithCanvasSnapshot(content string, canvasID string) error {
    // Generate snapshot via canvas-renderer plugin
    snapshotResp, err := p.callCanvasRenderer(canvasID)
    if err != nil {
        return errors.Wrap(err, "failed to generate canvas snapshot")
    }

    // Get IPFS URL
    ipfsURL := snapshotResp.URL

    // Create Bluesky post with link
    post := fmt.Sprintf("%s\n\n📊 Canvas: %s", content, ipfsURL)

    return p.createPost(post)
}
```

### 3.3 Documentation

**Create:** `docs/plugins/typescript.md` - How to write TypeScript plugins
**Create:** `docs/plugins/canvas-renderer.md` - Canvas renderer plugin docs
**Create:** `qntx-canvas-renderer/README.md` - Plugin-specific docs

### 3.4 Demo target integration

**Modify:** `Makefile`

```makefile
.PHONY: canvas-renderer-plugin
canvas-renderer-plugin:
	cd qntx-canvas-renderer && bun install

demo: web cli canvas-renderer-plugin ## Start QNTX in demo mode with TS plugins
	QNTX_DEMO=1 ./bin/qntx server --dev --no-browser --db-path demo.db -vvv &
	cd web && VITE_QNTX_DEMO=1 bun run dev &
```

### Phase 3 Success Criteria

- [ ] Can call `/api/canvas/snapshot?canvas_id=X` and get HTML
- [ ] HTML matches browser-rendered output
- [ ] Can pin snapshot to IPFS
- [ ] Bluesky posts include IPFS link to canvas snapshot
- [ ] Documentation is complete
- [ ] `make test` passes with TS plugin tests
- [ ] `make demo` starts with canvas renderer plugin

---

## Dependencies & Prerequisites

**Required:**
- Bun installed: `curl -fsSL https://bun.sh/install | bash`
- Protocol buffers compiler: `brew install protobuf`
- TypeScript protobuf plugin: `npm install -g ts-proto`

**Optional (Phase 3):**
- IPFS daemon running (for IPFS pinning)
- Bluesky account configured (for posting)

---

## Testing Strategy

### Unit Tests

**TypeScript (Bun test):**
- Plugin runtime gRPC service
- Canvas builder (shared code)
- CSS loading
- DOM environment setup

**Go (standard tests):**
- Plugin discovery for TS files
- Bun subprocess spawning
- gRPC communication

### Integration Tests

- Go server ↔ TS plugin communication
- HTTP handler registration
- Canvas rendering end-to-end
- CSS injection
- IPFS pinning

### Manual Testing

1. `make demo` - Start server with TS plugins
2. Create canvas with glyphs (note, code, prompt)
3. Export via client button (existing feature)
4. Call `/api/canvas/snapshot` (new feature)
5. Compare HTML output (should match)
6. Verify pan/zoom works in both
7. Test Bluesky posting with canvas link

---

## Migration Path

### Existing Client-Side Export (Unchanged)

```
User clicks Export button
  ↓
Browser captures DOM + CSS
  ↓
POST /api/canvas/export-dom
  ↓
Server writes to docs/demo/index.html
```

**Remains fast** - no subprocess, no plugin.

### New Server-Side Rendering

```
Server detects pattern → LLM generates insight
  ↓
POST /api/canvas/snapshot?canvas_id=X
  ↓
Call canvas-renderer TypeScript plugin
  ↓
Plugin builds HTML using shared TS code
  ↓
Pin to IPFS
  ↓
Include IPFS link in Bluesky post
```

**Both paths coexist.** Client path optimized for speed, server path enables automation.

---

## Future Extensions

Once TypeScript plugin infrastructure exists:

1. **Data processing plugins** - CSV transforms, JSON manipulation
2. **API integration plugins** - GitHub, Linear, Notion connectors
3. **Custom glyph renderers** - Specialized visualizations
4. **Template engines** - Mustache, Handlebars for content generation
5. **Site builder plugins** - Multi-page site generation from canvas

TypeScript plugins unlock the entire npm ecosystem for server-side automation.

---

## Open Questions

1. **CSS bundling:** Inline CSS files or read at runtime?
   - **Decision:** Read at runtime (simpler, smaller plugin)

2. **DOM library:** jsdom vs happy-dom?
   - **Decision:** jsdom (more mature, better compatibility)

3. **Plugin versioning:** How to handle plugin updates?
   - **Decision:** Treat like Python - plugin declares version, server checks compatibility

4. **Error handling:** How to surface plugin errors to Go server?
   - **Decision:** Use gRPC status codes + error messages in response

5. **Hot reload:** Should TS plugins reload on code changes in dev mode?
   - **Decision:** Phase 4 - not critical for MVP
